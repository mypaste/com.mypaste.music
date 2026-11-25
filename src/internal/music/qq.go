package music

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// qq talks to QQ Music's web search and to the musicu gateway. The gateway
// answers anonymous requests, so no cookie is needed; a song whose address the
// platform reserves comes back with an empty purl and the page says so.
type qq struct {
	fetch Fetcher
}

// qqTrack is one song row. Search answers it in data.song.list and the chart
// endpoint in songlist[].data, with the same field names.
type qqTrack struct {
	SongMID   string `json:"songmid"`
	SongName  string `json:"songname"`
	AlbumName string `json:"albumname"`
	AlbumMID  string `json:"albummid"`
	Interval  int    `json:"interval"`
	Singer    []struct {
		Name string `json:"name"`
	} `json:"singer"`
}

type qqSearch struct {
	Data struct {
		Song struct {
			List []qqTrack `json:"list"`
		} `json:"song"`
	} `json:"data"`
}

func qqSong(row qqTrack) Song {
	artists := make([]string, 0, len(row.Singer))
	for _, singer := range row.Singer {
		if name := cleanText(singer.Name); name != "" {
			artists = append(artists, name)
		}
	}
	song := Song{
		Source:   "qq",
		ID:       row.SongMID,
		Title:    cleanText(row.SongName),
		Artist:   strings.Join(artists, " / "),
		Album:    cleanText(row.AlbumName),
		Duration: row.Interval,
	}
	if row.AlbumMID != "" {
		song.Cover = "https://y.gtimg.cn/music/photo_new/T002R300x300M000" + row.AlbumMID + ".jpg"
	}
	return song
}

func (q *qq) Search(ctx context.Context, keyword string, limit int) ([]Song, error) {
	query := url.Values{}
	query.Set("w", keyword)
	query.Set("format", "json")
	query.Set("n", strconv.Itoa(positive(limit)))
	query.Set("p", "1")
	var answer qqSearch
	if err := fetchJSON(ctx, q.fetch, "GET",
		searchAddress("https://c.y.qq.com/soso/fcgi-bin/client_search_cp", query), nil, &answer); err != nil {
		return nil, err
	}
	songs := make([]Song, 0, len(answer.Data.Song.List))
	for _, row := range answer.Data.Song.List {
		songs = append(songs, qqSong(row))
	}
	return songs, nil
}

// qqCharts maps the closed chart words to the platform's toplist ids: 26 is
// 热歌榜, 27 is 新歌榜.
var qqCharts = map[string]int{"hot": 26, "new": 27}

type qqChart struct {
	SongList []struct {
		Data qqTrack `json:"data"`
	} `json:"songlist"`
}

// Chart reads a toplist in the order the platform publishes it.
func (q *qq) Chart(ctx context.Context, kind string, limit int) ([]Song, error) {
	topID, ok := qqCharts[kind]
	if !ok {
		return nil, errors.New("未知的榜单")
	}
	query := url.Values{}
	query.Set("topid", strconv.Itoa(topID))
	query.Set("format", "json")
	query.Set("song_begin", "0")
	query.Set("song_num", strconv.Itoa(positive(limit)))
	query.Set("type", "top")
	var answer qqChart
	if err := fetchJSON(ctx, q.fetch, "GET",
		searchAddress("https://c.y.qq.com/v8/fcg-bin/fcg_v8_toplist_cp.fcg", query), nil, &answer); err != nil {
		return nil, err
	}
	songs := make([]Song, 0, len(answer.SongList))
	for _, row := range answer.SongList {
		if len(songs) >= positive(limit) {
			break
		}
		song := qqSong(row.Data)
		if song.ID == "" || song.Title == "" {
			continue
		}
		songs = append(songs, song)
	}
	return songs, nil
}

// musicuCall is the gateway envelope: one module call, the common parameters
// the anonymous web client sends.
func musicuCall(module, method string, param any) ([]byte, error) {
	body := map[string]any{
		"req_0": map[string]any{
			"module": module, "method": method, "param": param,
		},
		"comm": map[string]any{"uin": 0, "format": "json", "ct": 24, "cv": 0},
	}
	return json.Marshal(body)
}

type qqVkey struct {
	Req struct {
		Data struct {
			SIP        []string `json:"sip"`
			MidURLInfo []struct {
				PURL string `json:"purl"`
			} `json:"midurlinfo"`
		} `json:"data"`
	} `json:"req_0"`
}

func (q *qq) Stream(ctx context.Context, song Song) (Stream, error) {
	if song.ID == "" {
		return Stream{}, errors.New("这首歌的编号无效")
	}
	// The gateway needs the file it should price, not just the song id: with
	// only songmid it answers a sip list and an empty purl even for a free
	// track. C400 is its 128 kbps m4a name, the one an anonymous web client
	// asks for.
	body, err := musicuCall("vkey.GetVkeyServer", "CgiGetVkey", map[string]any{
		"guid": "10000", "songmid": []string{song.ID}, "songtype": []int{0},
		"uin": "0", "loginflag": 1, "platform": "20",
		"filename": []string{"C400" + song.ID + ".m4a"},
	})
	if err != nil {
		return Stream{}, errors.New("无法编码播放请求")
	}
	var answer qqVkey
	if err := fetchJSON(ctx, q.fetch, "POST",
		"https://u.y.qq.com/cgi-bin/musicu.fcg", body, &answer); err != nil {
		return Stream{}, err
	}
	data := answer.Req.Data
	if len(data.SIP) == 0 || len(data.MidURLInfo) == 0 || data.MidURLInfo[0].PURL == "" {
		return Stream{}, errors.New("QQ 音乐没有给出播放地址（可能已下架，或需要会员）")
	}
	return Stream{URL: https(data.SIP[0] + data.MidURLInfo[0].PURL)}, nil
}

type qqLyric struct {
	Req struct {
		Data struct {
			Lyric string `json:"lyric"`
			Crypt int    `json:"crypt"`
		} `json:"data"`
	} `json:"req_0"`
}

func (q *qq) Lyric(ctx context.Context, song Song) (string, error) {
	body, err := musicuCall("music.musichallSong.PlayLyricInfo", "GetPlayLyricInfo", map[string]any{
		"songMID": song.ID, "songID": 0,
	})
	if err != nil {
		return "", errors.New("无法编码歌词请求")
	}
	var answer qqLyric
	if err := fetchJSON(ctx, q.fetch, "POST",
		"https://u.y.qq.com/cgi-bin/musicu.fcg", body, &answer); err != nil {
		return "", err
	}
	data := answer.Req.Data
	if data.Lyric == "" {
		return "", nil
	}
	// crypt != 0 is the platform's own encrypted lyric format; the page only
	// reads LRC, so an encrypted answer is treated as "no lyric".
	if data.Crypt != 0 {
		return "", nil
	}
	decoded, err := base64.StdEncoding.DecodeString(data.Lyric)
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(decoded)), nil
}
