package music

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// netease talks to the public web API of NetEase Cloud Music. No cookie and
// no signature is used, so a song that the platform reserves for a signed-in
// listener simply answers with an empty address; the page shows that answer
// as "this song has no playable address".
type netease struct {
	fetch Fetcher
}

type neteaseSearch struct {
	Result struct {
		Songs []struct {
			ID       int64 `json:"id"`
			Name     string
			Duration int `json:"duration"`
			Artists  []struct{ Name string }
			Album    struct{ Name string }
		} `json:"songs"`
	} `json:"result"`
}

type neteaseDetail struct {
	Songs []struct {
		ID    int64 `json:"id"`
		Album struct {
			PicURL string `json:"picUrl"`
		} `json:"album"`
	} `json:"songs"`
}

func (n *netease) Search(ctx context.Context, keyword string, limit int) ([]Song, error) {
	query := url.Values{}
	query.Set("s", keyword)
	query.Set("type", "1")
	query.Set("offset", "0")
	query.Set("limit", strconv.Itoa(positive(limit)))
	var answer neteaseSearch
	if err := fetchJSON(ctx, n.fetch, "GET",
		searchAddress("https://music.163.com/api/search/get/web", query), nil, &answer); err != nil {
		return nil, err
	}
	songs := make([]Song, 0, len(answer.Result.Songs))
	ids := make([]string, 0, len(answer.Result.Songs))
	for _, row := range answer.Result.Songs {
		artists := make([]string, 0, len(row.Artists))
		for _, artist := range row.Artists {
			if name := cleanText(artist.Name); name != "" {
				artists = append(artists, name)
			}
		}
		songs = append(songs, Song{
			Source:   "netease",
			ID:       strconv.FormatInt(row.ID, 10),
			Title:    cleanText(row.Name),
			Artist:   strings.Join(artists, " / "),
			Album:    cleanText(row.Album.Name),
			Duration: row.Duration / 1000,
		})
		ids = append(ids, strconv.FormatInt(row.ID, 10))
	}
	n.covers(ctx, songs, ids)
	return songs, nil
}

// covers fills album art from the detail endpoint, which is the one place the
// web API returns a picture URL. A failure here leaves the rows intact: a
// cover is decoration, not part of the result.
func (n *netease) covers(ctx context.Context, songs []Song, ids []string) {
	if len(ids) == 0 {
		return
	}
	query := url.Values{}
	query.Set("ids", "["+strings.Join(ids, ",")+"]")
	var detail neteaseDetail
	if err := fetchJSON(ctx, n.fetch, "GET",
		searchAddress("https://music.163.com/api/song/detail", query), nil, &detail); err != nil {
		return
	}
	pictures := make(map[int64]string, len(detail.Songs))
	for _, row := range detail.Songs {
		pictures[row.ID] = https(row.Album.PicURL)
	}
	for index := range songs {
		if id, err := strconv.ParseInt(songs[index].ID, 10, 64); err == nil {
			songs[index].Cover = pictures[id]
		}
	}
}

// neteaseCharts maps the closed chart words to the two playlists the web API
// publishes. 3778678 is 热歌榜 and 3779629 is 新歌榜.
var neteaseCharts = map[string]string{"hot": "3778678", "new": "3779629"}

type neteaseChart struct {
	Result struct {
		Tracks []struct {
			ID       int64  `json:"id"`
			Name     string `json:"name"`
			Duration int    `json:"duration"`
			Artists  []struct {
				Name string `json:"name"`
			} `json:"artists"`
			Album struct {
				Name   string `json:"name"`
				PicURL string `json:"picUrl"`
			} `json:"album"`
		} `json:"tracks"`
	} `json:"result"`
}

// Chart reads a playlist's tracks. The playlist answer already carries album
// pictures, so a chart needs no detail request; the page keeps the order the
// platform published, which is the ranking itself.
func (n *netease) Chart(ctx context.Context, kind string, limit int) ([]Song, error) {
	playlist, ok := neteaseCharts[kind]
	if !ok {
		return nil, errors.New("未知的榜单")
	}
	query := url.Values{}
	query.Set("id", playlist)
	var answer neteaseChart
	if err := fetchJSON(ctx, n.fetch, "GET",
		searchAddress("https://music.163.com/api/playlist/detail", query), nil, &answer); err != nil {
		return nil, err
	}
	songs := make([]Song, 0, len(answer.Result.Tracks))
	for _, row := range answer.Result.Tracks {
		if len(songs) >= positive(limit) {
			break
		}
		title := cleanText(row.Name)
		if row.ID == 0 || title == "" {
			continue
		}
		artists := make([]string, 0, len(row.Artists))
		for _, artist := range row.Artists {
			if name := cleanText(artist.Name); name != "" {
				artists = append(artists, name)
			}
		}
		songs = append(songs, Song{
			Source:   "netease",
			ID:       strconv.FormatInt(row.ID, 10),
			Title:    title,
			Artist:   strings.Join(artists, " / "),
			Album:    cleanText(row.Album.Name),
			Duration: row.Duration / 1000,
			Cover:    https(row.Album.PicURL),
		})
	}
	return songs, nil
}

type neteaseStream struct {
	Data []struct {
		URL string `json:"url"`
		BR  int    `json:"br"`
	} `json:"data"`
}

func (n *netease) Stream(ctx context.Context, song Song) (Stream, error) {
	id, err := strconv.ParseInt(song.ID, 10, 64)
	if err != nil {
		return Stream{}, errors.New("这首歌的编号无效")
	}
	query := url.Values{}
	query.Set("id", song.ID)
	query.Set("ids", fmt.Sprintf("[%d]", id))
	query.Set("br", "320000")
	var answer neteaseStream
	if err := fetchJSON(ctx, n.fetch, "GET",
		searchAddress("https://music.163.com/api/song/enhance/player/url", query), nil, &answer); err != nil {
		return Stream{}, err
	}
	if len(answer.Data) == 0 || !playable(answer.Data[0].URL) {
		return Stream{}, errors.New("网易云没有给出播放地址（可能已下架，或需要会员）")
	}
	return Stream{URL: https(answer.Data[0].URL), Quality: bitrate(answer.Data[0].BR)}, nil
}

func bitrate(value int) string {
	if value <= 0 {
		return ""
	}
	return strconv.Itoa(value/1000) + " kbps"
}

type neteaseLyric struct {
	LRC struct {
		Lyric string `json:"lyric"`
	} `json:"lrc"`
}

func (n *netease) Lyric(ctx context.Context, song Song) (string, error) {
	query := url.Values{}
	query.Set("id", song.ID)
	query.Set("lv", "1")
	query.Set("kv", "1")
	query.Set("tv", "-1")
	var answer neteaseLyric
	if err := fetchJSON(ctx, n.fetch, "GET",
		searchAddress("https://music.163.com/api/song/lyric", query), nil, &answer); err != nil {
		return "", err
	}
	return strings.TrimSpace(answer.LRC.Lyric), nil
}
