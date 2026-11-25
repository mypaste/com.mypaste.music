// Command mypaste-music is the music player's runtime.
//
// Protocol v3 lets a UI package declare a runtime and placement-less internal
// commands (D-1084); only this package's menu page can reach them, and they
// never appear in the host's command palette. The page draws the player; this
// process is the only thing that talks to the platforms, and only through the
// host's network.outbound proxy. Audio and cover bytes never pass through
// here: the page loads the addresses this process resolves.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mypaste/com.mypaste.music/src/internal/host"
	"github.com/mypaste/com.mypaste.music/src/internal/music"
)

// Version is the extension version written into archive manifests.
const Version = "1.4.0"

const commandRequest = "music.request"

// stateKey is the one file the player keeps in its private storage.
const stateKey = "state.json"

const (
	maxKeywordRunes = 80
	maxSearchLimit  = 50
	maxStateBytes   = 64 << 10
	maxFavorites    = 200
	maxRecent       = 40
	maxSearches     = 5
	maxTextBytes    = 512
)

type input struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type invocation struct {
	ID        string `json:"id"`
	Method    string `json:"method"`
	CommandID string `json:"commandId"`
	Deadline  int64  `json:"deadlineUnixMs"`
	Input     *input `json:"input,omitempty"`
}

type request struct {
	Action  string     `json:"action"`
	Source  string     `json:"source,omitempty"`
	Keyword string     `json:"keyword,omitempty"`
	Kind    string     `json:"kind,omitempty"`
	Limit   int        `json:"limit,omitempty"`
	Song    music.Song `json:"song,omitempty"`
	State   *state     `json:"state,omitempty"`
}

type state struct {
	Volume    float64      `json:"volume"`
	Source    string       `json:"source"`
	Favorites []music.Song `json:"favorites"`
	Recent    []music.Song `json:"recent"`
	Searches  []string     `json:"searches"`
	Repeat    string       `json:"repeat"`
	Shuffle   bool         `json:"shuffle"`
}

func defaultState() state {
	return state{Volume: 0.8, Source: "netease", Repeat: "all",
		Favorites: []music.Song{}, Recent: []music.Song{}, Searches: []string{}}
}

type answer struct {
	Songs   []music.Song `json:"songs,omitempty"`
	URL     string       `json:"url,omitempty"`
	Quality string       `json:"quality,omitempty"`
	// Song is present only when a failed platform address was rescued by the
	// aggregator and a different row is now playing; the page adopts it so
	// the cover, lyric and saved queue all name that row.
	Song  *music.Song `json:"song,omitempty"`
	Lyric string      `json:"lyric,omitempty"`
	State *state      `json:"state,omitempty"`
}

type failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type result struct {
	Output string `json:"output,omitempty"`
}

type response struct {
	ID     string   `json:"id"`
	OK     bool     `json:"ok"`
	Result *result  `json:"result,omitempty"`
	Error  *failure `json:"error,omitempty"`
}

func main() {
	client := host.NewClient(os.Stdin, os.Stdout)
	line, err := client.ReadRequestLine()
	if err != nil {
		os.Exit(1)
	}
	var call invocation
	if err := json.Unmarshal(line, &call); err != nil || call.ID == "" {
		os.Exit(1)
	}
	ctx, cancel := deadline(call.Deadline)
	defer cancel()
	output, runErr := run(ctx, client, call)
	if runErr != nil {
		write(client, response{ID: call.ID, OK: false,
			Error: &failure{Code: "runtime", Message: runErr.Error()}})
		return
	}
	write(client, response{ID: call.ID, OK: true, Result: &result{Output: output}})
}

// deadline keeps the runtime inside the host's own invocation window with a
// small margin, so the proxy failure that reaches the page is the actionable
// one instead of the host killing the process.
func deadline(deadlineUnixMs int64) (context.Context, context.CancelFunc) {
	const margin = 2 * time.Second
	if deadlineUnixMs <= 0 {
		return context.WithTimeout(context.Background(), 30*time.Second)
	}
	window := time.Until(time.UnixMilli(deadlineUnixMs))
	if window > margin {
		window -= margin
	}
	if window <= 0 {
		window = time.Second
	}
	return context.WithTimeout(context.Background(), window)
}

func run(ctx context.Context, client *host.Client, call invocation) (string, error) {
	if call.Method != "invoke" || call.CommandID != commandRequest {
		return "", errors.New("这个扩展只提供音乐页面使用的一个操作")
	}
	if call.Input == nil || call.Input.Type != "text/plain" || strings.TrimSpace(call.Input.Text) == "" {
		return "", errors.New("请求内容为空")
	}
	var asked request
	if err := json.Unmarshal([]byte(call.Input.Text), &asked); err != nil {
		return "", errors.New("请求内容无法解析")
	}
	sources, err := music.New(client.Fetch)
	if err != nil {
		return "", err
	}
	fallback := music.NewFallback(client.Fetch)
	switch asked.Action {
	case "state.load":
		current, err := loadState(ctx, client)
		if err != nil {
			return "", err
		}
		return encode(answer{State: &current})
	case "state.save":
		if asked.State == nil {
			return "", errors.New("没有可保存的状态")
		}
		current := normalizeState(*asked.State)
		data, err := json.Marshal(current)
		if err != nil || len(data) > maxStateBytes {
			return "", errors.New("播放状态过大")
		}
		if err := client.StorageWrite(ctx, stateKey, data); err != nil {
			return "", err
		}
		return encode(answer{State: &current})
	case "search":
		source, ok := sources[asked.Source]
		if !ok {
			return "", errors.New("未知的音乐平台")
		}
		keyword := strings.TrimSpace(asked.Keyword)
		if keyword == "" || utf8.RuneCountInString(keyword) > maxKeywordRunes {
			return "", errors.New("请输入 1–80 个字的搜索词")
		}
		limit := asked.Limit
		if limit < 1 || limit > maxSearchLimit {
			limit = 30
		}
		songs, searchErr := source.Search(ctx, keyword, limit)
		// A platform that answers nothing (blocked, rate-limited, or simply
		// without this song) does not end the search: the aggregator is asked
		// once, and its rows carry the routing that plays them later.
		if searchErr != nil || len(songs) == 0 {
			if alt, altErr := fallback.Search(ctx, keyword, limit); altErr == nil {
				songs = alt
			}
		}
		if songs == nil {
			if searchErr != nil {
				return "", searchErr
			}
			songs = []music.Song{}
		}
		return encode(answer{Songs: songs})
	case "chart":
		source, ok := sources[asked.Source]
		if !ok {
			return "", errors.New("未知的音乐平台")
		}
		if !music.KnownChart(asked.Kind) {
			return "", errors.New("未知的榜单")
		}
		limit := asked.Limit
		if limit < 1 || limit > maxSearchLimit {
			limit = 30
		}
		songs, err := source.Chart(ctx, asked.Kind, limit)
		if err != nil {
			return "", err
		}
		if songs == nil {
			songs = []music.Song{}
		}
		return encode(answer{Songs: songs})
	case "stream":
		source, ok := sources[asked.Source]
		if !ok {
			return "", errors.New("未知的音乐平台")
		}
		if err := validSong(asked.Song); err != nil {
			return "", err
		}
		var stream music.Stream
		var rescued *music.Song
		if asked.Song.Via == music.ViaFallback {
			stream, err = fallback.Stream(ctx, asked.Song)
		} else {
			stream, err = source.Stream(ctx, asked.Song)
			// A platform that cannot answer an address (region, membership,
			// or a row that is gone) does not end playback: the aggregator is
			// asked for the same track, and the row it answers is what plays.
			if err == nil && strings.TrimSpace(stream.URL) == "" {
				err = errors.New("平台没有给出播放地址")
			}
			if err != nil {
				platformErr := err
				matched, alt, altErr := fallback.Rescue(ctx, asked.Song)
				if altErr != nil {
					return "", platformErr
				}
				stream, rescued, err = alt, &matched, nil
			}
		}
		if err != nil {
			return "", err
		}
		return encode(answer{URL: stream.URL, Quality: stream.Quality, Song: rescued})
	case "lyric":
		source, ok := sources[asked.Source]
		if !ok {
			return "", errors.New("未知的音乐平台")
		}
		if err := validSong(asked.Song); err != nil {
			return "", err
		}
		var lyric string
		if asked.Song.Via == music.ViaFallback {
			lyric, err = fallback.Lyric(ctx, asked.Song)
		} else {
			lyric, err = source.Lyric(ctx, asked.Song)
		}
		if err != nil {
			return "", err
		}
		return encode(answer{Lyric: lyric})
	default:
		return "", errors.New("未知的操作：" + asked.Action)
	}
}

func validSong(song music.Song) error {
	if strings.TrimSpace(song.ID) == "" || len(song.ID) > maxTextBytes {
		return errors.New("这首歌的编号无效")
	}
	if len(song.Title) > maxTextBytes || len(song.Artist) > maxTextBytes {
		return errors.New("这首歌的信息过长")
	}
	return nil
}

func loadState(ctx context.Context, client *host.Client) (state, error) {
	data, err := client.StorageRead(ctx, stateKey)
	if errors.Is(err, host.ErrMissingKey) {
		return defaultState(), nil
	}
	if err != nil {
		return state{}, err
	}
	if len(data) > maxStateBytes {
		return defaultState(), nil
	}
	var stored state
	if err := json.Unmarshal(data, &stored); err != nil {
		return defaultState(), nil
	}
	return normalizeState(stored), nil
}

// normalizeState is the one owner of what the page may persist: a known
// source, a bounded volume, a known repeat mode, and bounded lists. A state
// file written by an older version (or hand-edited) never reaches the page
// unchanged.
func normalizeState(stored state) state {
	current := state{
		Volume:  stored.Volume,
		Source:  stored.Source,
		Repeat:  stored.Repeat,
		Shuffle: stored.Shuffle,
	}
	if current.Volume < 0 || current.Volume > 1 {
		current.Volume = 0.8
	}
	known := false
	for _, name := range music.Names {
		if current.Source == name {
			known = true
		}
	}
	if !known {
		current.Source = "netease"
	}
	switch current.Repeat {
	case "all", "one", "off":
	default:
		current.Repeat = "all"
	}
	current.Favorites = bounded(stored.Favorites, maxFavorites)
	current.Recent = bounded(stored.Recent, maxRecent)
	current.Searches = recentSearches(stored.Searches)
	return current
}

// recentSearches keeps the newest few queries, most recent first: trimmed,
// without blanks, repeats or oversized keywords. The page sends the list in
// that order; the runtime only enforces its bounds, so a hand-edited file
// cannot grow the search tab or smuggle a paragraph into a row.
func recentSearches(queries []string) []string {
	kept := make([]string, 0, maxSearches)
	seen := make(map[string]bool, len(queries))
	for _, query := range queries {
		query = strings.TrimSpace(query)
		if query == "" || utf8.RuneCountInString(query) > maxKeywordRunes || seen[query] {
			continue
		}
		seen[query] = true
		kept = append(kept, query)
		if len(kept) == maxSearches {
			break
		}
	}
	return kept
}

// bounded keeps the first rows that carry a known source and an id. It drops
// rather than truncates: one malformed row must not cost the rows after it.
func bounded(songs []music.Song, limit int) []music.Song {
	kept := make([]music.Song, 0, limit)
	known := make(map[string]bool, len(music.Names))
	for _, name := range music.Names {
		known[name] = true
	}
	for _, song := range songs {
		if len(kept) == limit {
			break
		}
		if !known[song.Source] || strings.TrimSpace(song.ID) == "" ||
			len(song.ID) > maxTextBytes || len(song.Title) > maxTextBytes {
			continue
		}
		kept = append(kept, song)
	}
	return kept
}

func encode(value answer) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", errors.New("无法编码结果")
	}
	return string(encoded), nil
}

func write(client *host.Client, payload response) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		os.Exit(1)
	}
	if err := client.WriteLine(encoded); err != nil {
		os.Exit(1)
	}
}
