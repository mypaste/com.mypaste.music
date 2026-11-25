package music

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// ViaFallback marks a song whose address and lyric are resolved through the
// aggregator rather than the platform's own API. The rows still belong to a
// platform (NetEase), so their `source` stays `netease` and the page treats
// them like any other row; only this word decides who answers stream/lyric.
const ViaFallback = "gdstudio"

// fallbackAddress is the aggregator's single endpoint. It answers search,
// url, lyric and pic requests by the `types` query. It is a last resort: the
// runtime asks the platform first and only comes here when that search has
// nothing to show.
const fallbackAddress = "https://music-api.gdstudio.xyz/api.php"

// Fallback is the aggregator client. Search rows carry the ids this endpoint
// itself needs later (`url_id`, `lyric_id`), which is why they are kept on
// Song rather than looked up again by the page.
type Fallback struct {
	fetch Fetcher
}

// NewFallback builds the aggregator client. Like New, a missing proxy is a
// wiring bug.
func NewFallback(fetch Fetcher) *Fallback {
	return &Fallback{fetch: fetch}
}

type fallbackRow struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Artist  []string `json:"artist"`
	Album   string   `json:"album"`
	URLID   string   `json:"url_id"`
	LyricID string   `json:"lyric_id"`
	Source  string   `json:"source"`
}

// Search asks the aggregator's NetEase source. Only that source is offered:
// the public deployment refuses the other Chinese platforms, and the songs it
// returns are NetEase songs, so their album art is filled by NetEase's own
// detail endpoint instead of one extra call per row.
func (f *Fallback) Search(ctx context.Context, keyword string, limit int) ([]Song, error) {
	query := url.Values{}
	query.Set("types", "search")
	query.Set("source", "netease")
	query.Set("name", keyword)
	query.Set("count", strconv.Itoa(positive(limit)))
	query.Set("pages", "1")
	var rows []fallbackRow
	if err := fetchJSON(ctx, f.fetch, "GET",
		searchAddress(fallbackAddress, query), nil, &rows); err != nil {
		return nil, err
	}
	songs := make([]Song, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		id := strings.TrimSpace(row.ID)
		if id == "" {
			continue
		}
		artists := make([]string, 0, len(row.Artist))
		for _, artist := range row.Artist {
			if name := cleanText(artist); name != "" {
				artists = append(artists, name)
			}
		}
		song := Song{
			Source:   "netease",
			ID:       id,
			Title:    cleanText(row.Name),
			Artist:   strings.Join(artists, " / "),
			Album:    cleanText(row.Album),
			Via:      ViaFallback,
			StreamID: firstNonEmpty(row.URLID, id),
			LyricID:  firstNonEmpty(row.LyricID, id),
		}
		songs = append(songs, song)
		ids = append(ids, id)
		if len(songs) == positive(limit) {
			break
		}
	}
	(&netease{fetch: f.fetch}).covers(ctx, songs, ids)
	return songs, nil
}

type fallbackStream struct {
	URL string `json:"url"`
	BR  int    `json:"br"`
}

func (f *Fallback) Stream(ctx context.Context, song Song) (Stream, error) {
	query := url.Values{}
	query.Set("types", "url")
	query.Set("source", "netease")
	query.Set("id", firstNonEmpty(song.StreamID, song.ID))
	query.Set("br", "320")
	var answer fallbackStream
	if err := fetchJSON(ctx, f.fetch, "GET",
		searchAddress(fallbackAddress, query), nil, &answer); err != nil {
		return Stream{}, err
	}
	if !playable(answer.URL) {
		return Stream{}, errors.New("备用接口没有给出播放地址（可能已下架，或需要会员）")
	}
	return Stream{URL: https(answer.URL), Quality: bitrate(answer.BR * 1000)}, nil
}

// Rescue is the playback fallback. When the platform's own address lookup
// fails, the aggregator is searched by the song's title and artist and the row
// that names the same track is resolved. The matched row is returned with the
// stream so the page can adopt the ids, cover and lyric that actually play.
func (f *Fallback) Rescue(ctx context.Context, want Song) (Song, Stream, error) {
	keyword := strings.TrimSpace(want.Title + " " + want.Artist)
	if keyword == "" {
		return Song{}, Stream{}, errors.New("没有可以用来找这首歌的歌名")
	}
	rows, err := f.Search(ctx, keyword, 10)
	if err != nil {
		return Song{}, Stream{}, err
	}
	matched, found := closest(want, rows)
	if !found {
		return Song{}, Stream{}, errors.New("备用接口没有找到同一首歌")
	}
	stream, err := f.Stream(ctx, matched)
	if err != nil {
		return Song{}, Stream{}, err
	}
	return matched, stream, nil
}

// closest picks the aggregator row that names the same track. A folded title
// must meet (an answer for a different song is not a rescue), and a shared
// artist raises that row above a plain title hit.
func closest(want Song, rows []Song) (Song, bool) {
	title := foldTitle(want.Title)
	if title == "" {
		return Song{}, false
	}
	artists := artistNames(want.Artist)
	best := Song{}
	bestScore := 0
	for _, row := range rows {
		other := foldTitle(row.Title)
		score := 0
		switch {
		case other == title:
			score = 2
		case other != "" && (strings.Contains(other, title) || strings.Contains(title, other)):
			score = 1
		}
		if score == 0 {
			continue
		}
		for name := range artistNames(row.Artist) {
			if artists[name] {
				score += 2
				break
			}
		}
		if score > bestScore {
			best, bestScore = row, score
		}
	}
	return best, bestScore > 0
}

// foldTitle makes the same track compare equal across platforms: case and
// spacing do not matter, a bracketed variant tag (Live, Remix, 伴奏) is
// ignored, and the hyphens platforms sprinkle into titles are dropped.
func foldTitle(title string) string {
	title = strings.ToLower(strings.TrimSpace(title))
	if cut := strings.IndexAny(title, "(（[【"); cut > 0 {
		title = title[:cut]
	}
	var folded strings.Builder
	for _, r := range title {
		if unicode.IsSpace(r) || strings.ContainsRune("·・-–—_", r) {
			continue
		}
		folded.WriteRune(r)
	}
	return folded.String()
}

// artistNames splits the page's joined artist line back into names. Matching
// needs one name in common, not the whole line, because platforms disagree
// about who gets top billing.
func artistNames(artist string) map[string]bool {
	names := make(map[string]bool)
	for _, part := range strings.FieldsFunc(artist, func(r rune) bool {
		return strings.ContainsRune("/、,&；;", r)
	}) {
		if name := strings.ToLower(strings.TrimSpace(part)); name != "" {
			names[name] = true
		}
	}
	return names
}

type fallbackLyric struct {
	Lyric string `json:"lyric"`
}

func (f *Fallback) Lyric(ctx context.Context, song Song) (string, error) {
	query := url.Values{}
	query.Set("types", "lyric")
	query.Set("source", "netease")
	query.Set("id", firstNonEmpty(song.LyricID, song.ID))
	var answer fallbackLyric
	if err := fetchJSON(ctx, f.fetch, "GET",
		searchAddress(fallbackAddress, query), nil, &answer); err != nil {
		return "", err
	}
	return strings.TrimSpace(answer.Lyric), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
