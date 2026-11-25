package music

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// kuwo talks to Kuwo's web search, its address converter and its mobile lyric
// page. The search endpoint answers JSON written with single quotes; the one
// conversion in normalizeQuotes below is the smallest reader that accepts it.
type kuwo struct {
	fetch Fetcher
}

type kuwoSearch struct {
	Abslist []struct {
		MusicRID     string `json:"MUSICRID"`
		TargetID     string `json:"DC_TARGETID"`
		SongName     string `json:"SONGNAME"`
		Name         string `json:"NAME"`
		Artist       string `json:"ARTIST"`
		AlbumArtist  string `json:"AARTIST"`
		Album        string `json:"ALBUM"`
		Duration     string `json:"DURATION"`
		AlbumPicture string `json:"web_albumpic_short"`
	} `json:"abslist"`
}

func (k *kuwo) Search(ctx context.Context, keyword string, limit int) ([]Song, error) {
	query := url.Values{}
	query.Set("all", keyword)
	query.Set("ft", "music")
	query.Set("itemset", "web_2013")
	query.Set("client", "kt")
	query.Set("pn", "0")
	query.Set("rn", strconv.Itoa(positive(limit)))
	query.Set("rformat", "json")
	query.Set("encoding", "utf8")
	status, data, err := k.fetch(ctx, "GET",
		searchAddress("https://search.kuwo.cn/r.s", query), nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status > 299 {
		return nil, fmt.Errorf("平台返回了 %d", status)
	}
	var answer kuwoSearch
	if err := json.Unmarshal(normalizeQuotes(data), &answer); err != nil {
		return nil, errors.New("平台的回答无法解析")
	}
	songs := make([]Song, 0, len(answer.Abslist))
	for _, row := range answer.Abslist {
		id := kuwoID(row.MusicRID, row.TargetID)
		if id == "" {
			continue
		}
		title := cleanText(row.Name)
		if title == "" {
			title = cleanText(row.SongName)
		}
		if title == "" {
			continue
		}
		artist := cleanText(row.Artist)
		if artist == "" {
			artist = cleanText(row.AlbumArtist)
		}
		song := Song{
			Source:   "kuwo",
			ID:       id,
			Title:    title,
			Artist:   artist,
			Album:    cleanText(row.Album),
			Duration: atoi(row.Duration),
		}
		if row.AlbumPicture != "" {
			song.Cover = "https://img1.kuwo.cn/star/albumcover/" + strings.TrimLeft(row.AlbumPicture, "/")
		}
		songs = append(songs, song)
	}
	return songs, nil
}

// kuwoCharts maps the closed chart words to the platform's bang ids: 16 is
// 热歌榜, 17 is 新歌榜.
var kuwoCharts = map[string]string{"hot": "16", "new": "17"}

type kuwoChart struct {
	MusicList []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Artist   string `json:"artist"`
		Album    string `json:"album"`
		Duration string `json:"song_duration"`
	} `json:"musiclist"`
}

// Chart reads a bang through the legacy ksong endpoint, which is the only one
// that answers an anonymous reader: the mobile API wants a CSRF header and the
// host proxy sends none. The rows carry no album picture, so a chart row keeps
// the placeholder art.
func (k *kuwo) Chart(ctx context.Context, kind string, limit int) ([]Song, error) {
	bang, ok := kuwoCharts[kind]
	if !ok {
		return nil, errors.New("未知的榜单")
	}
	query := url.Values{}
	query.Set("from", "pc")
	query.Set("fmt", "json")
	query.Set("pn", "0")
	query.Set("rn", strconv.Itoa(positive(limit)))
	query.Set("type", "bang")
	query.Set("data", "content")
	query.Set("id", bang)
	query.Set("show_copyright_off", "0")
	query.Set("pcmp4", "1")
	query.Set("isbang", "1")
	query.Set("pcjson", "1")
	status, data, err := k.fetch(ctx, "GET",
		searchAddress("https://kbangserver.kuwo.cn/ksong.s", query), nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status > 299 {
		return nil, fmt.Errorf("平台返回了 %d", status)
	}
	var answer kuwoChart
	if err := json.Unmarshal(data, &answer); err != nil {
		return nil, errors.New("平台的回答无法解析")
	}
	songs := make([]Song, 0, len(answer.MusicList))
	for _, row := range answer.MusicList {
		if len(songs) >= positive(limit) {
			break
		}
		id := strings.TrimSpace(row.ID)
		title := cleanText(row.Name)
		if id == "" || title == "" {
			continue
		}
		songs = append(songs, Song{
			Source:   "kuwo",
			ID:       id,
			Title:    title,
			Artist:   cleanText(row.Artist),
			Album:    cleanText(row.Album),
			Duration: atoi(row.Duration),
		})
	}
	return songs, nil
}

// kuwoID prefers MUSICRID ("MUSIC_123") and falls back to DC_TARGETID. The
// player only needs the bare number for both the converter and the lyric page.
func kuwoID(musicRID, targetID string) string {
	if id := strings.TrimPrefix(strings.TrimSpace(musicRID), "MUSIC_"); id != "" {
		return id
	}
	return strings.TrimSpace(targetID)
}

func (k *kuwo) Stream(ctx context.Context, song Song) (Stream, error) {
	query := url.Values{}
	query.Set("type", "convert_url")
	query.Set("format", "mp3")
	query.Set("response", "url")
	query.Set("rid", "MUSIC_"+song.ID)
	status, data, err := k.fetch(ctx, "GET",
		searchAddress("https://antiserver.kuwo.cn/anti.s", query), nil)
	if err != nil {
		return Stream{}, err
	}
	if status < 200 || status > 299 {
		return Stream{}, fmt.Errorf("平台返回了 %d", status)
	}
	address := strings.TrimSpace(string(data))
	if !playable(address) {
		return Stream{}, errors.New("酷我没有给出播放地址（可能已下架，或需要会员）")
	}
	return Stream{URL: https(address)}, nil
}

type kuwoLyric struct {
	Data *struct {
		LRClist []struct {
			LineLyric string `json:"lineLyric"`
			Time      string `json:"time"`
		} `json:"lrclist"`
	} `json:"data"`
}

func (k *kuwo) Lyric(ctx context.Context, song Song) (string, error) {
	query := url.Values{}
	query.Set("musicId", song.ID)
	status, data, err := k.fetch(ctx, "GET",
		searchAddress("https://m.kuwo.cn/newh5/singles/songinfoandlrc", query), nil)
	if err != nil {
		return "", err
	}
	if status < 200 || status > 299 {
		return "", fmt.Errorf("平台返回了 %d", status)
	}
	var answer kuwoLyric
	if err := json.Unmarshal(data, &answer); err != nil || answer.Data == nil {
		return "", nil
	}
	type line struct {
		second float64
		text   string
	}
	lines := make([]line, 0, len(answer.Data.LRClist))
	for _, row := range answer.Data.LRClist {
		text := strings.TrimSpace(row.LineLyric)
		if text == "" {
			continue
		}
		second, err := strconv.ParseFloat(row.Time, 64)
		if err != nil || second < 0 {
			continue
		}
		lines = append(lines, line{second: second, text: text})
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].second < lines[j].second })
	var lyric strings.Builder
	for _, row := range lines {
		minute := int(row.second) / 60
		rest := row.second - float64(minute*60)
		fmt.Fprintf(&lyric, "[%02d:%05.2f]%s\n", minute, rest, row.text)
	}
	return strings.TrimSpace(lyric.String()), nil
}

// normalizeQuotes rewrites the single-quoted strings of Kuwo's search answer
// into JSON strings. It walks the bytes instead of matching a pattern because
// a title may itself contain an apostrophe: a quote only closes the current
// string when the next meaningful byte starts a new token, which is what the
// format's own punctuation does. Numbers, braces and bare words pass through.
func normalizeQuotes(data []byte) []byte {
	out := make([]byte, 0, len(data)+16)
	inString := false
	for index := 0; index < len(data); index++ {
		char := data[index]
		if !inString {
			if char == '\'' {
				out = append(out, '"')
				inString = true
				continue
			}
			out = append(out, char)
			continue
		}
		switch char {
		case '\\':
			out = append(out, char)
			if index+1 < len(data) {
				index++
				out = append(out, data[index])
			}
		case '\'':
			if closesString(data[index+1:]) {
				out = append(out, '"')
				inString = false
			} else {
				out = append(out, char)
			}
		case '"':
			out = append(out, '\\', '"')
		default:
			out = append(out, char)
		}
	}
	return out
}

// closesString reports whether the bytes after a quote start the token that
// follows a finished string. A comma, a closing bracket or the end of the
// document does; a letter (as in Don't) does not.
func closesString(rest []byte) bool {
	for _, char := range rest {
		switch char {
		case ' ', '\t', '\r', '\n':
			continue
		case ',', ':', ']', '}':
			return true
		default:
			return false
		}
	}
	return true
}
