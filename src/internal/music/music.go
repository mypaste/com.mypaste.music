// Package music holds the three platform clients the player offers.
//
// A client turns a keyword into songs, a song into one playable address, and
// a song into its lyric text. Every request leaves through the host's
// network.outbound proxy: the manifest declares the API hosts, the proxy owns
// TLS, and this package never opens a socket. The media and cover addresses a
// client returns are loaded by the page itself, under surface.network.
package music

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Song is one result row. ID is the platform's own identifier: a numeric id
// for NetEase, a songmid for QQ Music, a music id for Kuwo.
type Song struct {
	Source   string `json:"source"`
	ID       string `json:"id"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album,omitempty"`
	Duration int    `json:"duration,omitempty"`
	Cover    string `json:"cover,omitempty"`
	// Via is empty for a platform's own API and ViaFallback for a row the
	// aggregator produced; it decides who resolves the stream and lyric.
	Via      string `json:"via,omitempty"`
	StreamID string `json:"streamId,omitempty"`
	LyricID  string `json:"lyricId,omitempty"`
}

// Stream is one resolved, directly playable address.
type Stream struct {
	URL     string `json:"url"`
	Quality string `json:"quality,omitempty"`
}

// Fetcher performs one HTTPS request through the host proxy.
type Fetcher func(ctx context.Context, method, url string, body []byte) (int, []byte, error)

// Source is one platform client.
type Source interface {
	Search(ctx context.Context, keyword string, limit int) ([]Song, error)
	Chart(ctx context.Context, kind string, limit int) ([]Song, error)
	Stream(ctx context.Context, song Song) (Stream, error)
	Lyric(ctx context.Context, song Song) (string, error)
}

// Names is the closed list of platforms, in the order the page offers them.
var Names = []string{"netease", "qq", "kuwo"}

// ChartKinds is the closed list of charts every platform publishes: the hot
// chart and the new-songs chart. A client maps these words to its own ids.
var ChartKinds = []string{"hot", "new"}

// KnownChart reports whether kind is one of ChartKinds.
func KnownChart(kind string) bool {
	for _, name := range ChartKinds {
		if kind == name {
			return true
		}
	}
	return false
}

// New builds the clients. The names are closed: an unknown one is a bug, not
// a user input.
func New(fetch Fetcher) (map[string]Source, error) {
	if fetch == nil {
		return nil, errors.New("缺少网络通道")
	}
	return map[string]Source{
		"netease": &netease{fetch: fetch},
		"qq":      &qq{fetch: fetch},
		"kuwo":    &kuwo{fetch: fetch},
	}, nil
}

// fetchJSON performs one request and decodes a JSON answer. Non-2xx answers
// are reported with their status, never with their body: a platform error
// page can be long and says nothing the reader can act on.
func fetchJSON(ctx context.Context, fetch Fetcher, method, address string, body []byte, target any) error {
	status, data, err := fetch(ctx, method, address, body)
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		return fmt.Errorf("平台返回了 %d", status)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return errors.New("平台的回答无法解析")
	}
	return nil
}

// https upgrades a platform-returned address. The page is a document on the
// app's own scheme; its media policy allows both schemes, but the CDNs all
// answer on HTTPS and one mixed-content rule should never be the reason a
// song will not start.
func https(address string) string {
	address = strings.TrimSpace(address)
	if strings.HasPrefix(address, "http://") {
		return "https://" + strings.TrimPrefix(address, "http://")
	}
	return address
}

func playable(address string) bool {
	address = strings.TrimSpace(address)
	return strings.HasPrefix(address, "https://") || strings.HasPrefix(address, "http://")
}

func searchAddress(base string, query url.Values) string {
	return base + "?" + query.Encode()
}

func positive(value int) int {
	if value < 1 {
		return 1
	}
	return value
}

func atoi(text string) int {
	value, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || value < 0 {
		return 0
	}
	return value
}
