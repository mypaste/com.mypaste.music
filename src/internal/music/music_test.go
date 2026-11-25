package music

import (
	"context"
	"strings"
	"testing"
)

// stub answers requests by URL fragment, so one test can pin a whole
// provider conversation without a socket.
type stub map[string]func(method, url string, body []byte) (int, []byte, error)

func (s stub) fetch(ctx context.Context, method, url string, body []byte) (int, []byte, error) {
	for fragment, answer := range s {
		if strings.Contains(url, fragment) {
			return answer(method, url, body)
		}
	}
	return 404, nil, nil
}

func fixed(status int, body string) func(string, string, []byte) (int, []byte, error) {
	return func(string, string, []byte) (int, []byte, error) {
		return status, []byte(body), nil
	}
}

func TestNeteaseSearchAddsCoversAndSeconds(t *testing.T) {
	source := &netease{fetch: stub{
		"/api/search/get/web": fixed(200, `{"result":{"songs":[
			{"id":26659435,"name":"Indie a","duration":319039,
			 "artists":[{"name":"A"},{"name":"B"}],"album":{"name":"Album"}},
			{"id":2,"name":"Two","duration":1000,"artists":[{"name":"C"}],"album":{"name":""}}]}}`),
		"/api/song/detail": fixed(200, `{"songs":[{"id":26659435,"album":{"picUrl":"http://p1.music.126.net/a.jpg"}}]}`),
	}.fetch}
	songs, err := source.Search(context.Background(), "indie", 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) != 2 {
		t.Fatalf("songs = %#v", songs)
	}
	if songs[0].ID != "26659435" || songs[0].Artist != "A / B" || songs[0].Duration != 319 {
		t.Fatalf("first = %#v", songs[0])
	}
	if songs[0].Cover != "https://p1.music.126.net/a.jpg" {
		t.Fatalf("cover = %q", songs[0].Cover)
	}
	if songs[1].Cover != "" {
		t.Fatalf("missing picture became %q", songs[1].Cover)
	}
}

func TestNeteaseStreamUpgradesAndRefusesAnEmptyAddress(t *testing.T) {
	source := &netease{fetch: stub{
		"/player/url": fixed(200, `{"data":[{"url":"http://m10.music.126.net/a.mp3","br":320000}]}`),
	}.fetch}
	stream, err := source.Stream(context.Background(), Song{ID: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if stream.URL != "https://m10.music.126.net/a.mp3" || stream.Quality != "320 kbps" {
		t.Fatalf("stream = %#v", stream)
	}

	empty := &netease{fetch: stub{
		"/player/url": fixed(200, `{"data":[{"url":null,"br":0}]}`),
	}.fetch}
	if _, err := empty.Stream(context.Background(), Song{ID: "1"}); err == nil {
		t.Fatal("an empty address must be a readable error")
	}
}

func TestQQLyricDecodesBase64AndSkipsEncrypted(t *testing.T) {
	plain := &qq{fetch: stub{
		"musicu.fcg": fixed(200, `{"req_0":{"data":{"lyric":"WzAwOjAwLjAwXWhp","crypt":0}}}`),
	}.fetch}
	lyric, err := plain.Lyric(context.Background(), Song{ID: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if lyric != "[00:00.00]hi" {
		t.Fatalf("lyric = %q", lyric)
	}

	encrypted := &qq{fetch: stub{
		"musicu.fcg": fixed(200, `{"req_0":{"data":{"lyric":"WzAwOjAwLjAwXWhp","crypt":1}}}`),
	}.fetch}
	lyric, err = encrypted.Lyric(context.Background(), Song{ID: "x"})
	if err != nil || lyric != "" {
		t.Fatalf("encrypted lyric = %q, %v", lyric, err)
	}
}

func TestQQStreamBuildsTheAddressFromSIPAndPURL(t *testing.T) {
	var asked []byte
	source := &qq{fetch: stub{
		"musicu.fcg": func(method, url string, body []byte) (int, []byte, error) {
			asked = body
			return 200, []byte(`{"req_0":{"data":{"sip":["http://aqqmusic.tc.qq.com/"],
				"midurlinfo":[{"purl":"C400.m4a?vkey=1"}]}}}`), nil
		},
	}.fetch}
	stream, err := source.Stream(context.Background(), Song{ID: "mid"})
	if err != nil {
		t.Fatal(err)
	}
	if stream.URL != "https://aqqmusic.tc.qq.com/C400.m4a?vkey=1" {
		t.Fatalf("stream = %#v", stream)
	}
	// Without the filename the gateway prices nothing and answers an empty
	// purl even for a free track.
	if !strings.Contains(string(asked), `"filename":["C400mid.m4a"]`) {
		t.Fatalf("request did not name the file: %s", asked)
	}
}

func TestKuwoSearchReadsSingleQuotedRowsAndKeepsApostrophes(t *testing.T) {
	body := `{'abslist':[
	 {'MUSICRID':'MUSIC_228908','DC_TARGETID':'228908','NAME':'Don't Stop','ARTIST':'A&B','ALBUM':'','DURATION':'210','web_albumpic_short':''},
	 {'MUSICRID':'MUSIC_5','DC_TARGETID':'5','NAME':'晴天&nbsp;(Live)','ARTIST':'C','ALBUM':'X','DURATION':'269'}]}`
	source := &kuwo{fetch: stub{"/r.s": fixed(200, body)}.fetch}
	songs, err := source.Search(context.Background(), "x", 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) != 2 {
		t.Fatalf("songs = %#v", songs)
	}
	if songs[0].Title != "Don't Stop" || songs[0].Artist != "A&B" || songs[0].ID != "228908" || songs[0].Duration != 210 {
		t.Fatalf("first = %#v", songs[0])
	}
	if songs[1].Title != "晴天 (Live)" {
		t.Fatalf("second = %#v", songs[1])
	}
}

func TestNeteaseChartReadsPlaylistTracksInOrder(t *testing.T) {
	var asked string
	source := &netease{fetch: stub{
		"/api/playlist/detail": func(method, url string, body []byte) (int, []byte, error) {
			asked = url
			return 200, []byte(`{"result":{"tracks":[
				{"id":9,"name":"Two","duration":210000,"artists":[{"name":"B"}],
				 "album":{"name":"Al","picUrl":"http://p1.music.126.net/b.jpg"}},
				{"id":8,"name":"One","duration":1000,"artists":[{"name":"A"},{"name":"C"}],
				 "album":{"name":"","picUrl":""}}]}}`), nil
		},
	}.fetch}
	songs, err := source.Chart(context.Background(), "hot", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(asked, "id=3778678") {
		t.Fatalf("hot chart asked %q", asked)
	}
	if len(songs) != 1 {
		t.Fatalf("limit ignored: %#v", songs)
	}
	if songs[0].ID != "9" || songs[0].Title != "Two" || songs[0].Duration != 210 ||
		songs[0].Cover != "https://p1.music.126.net/b.jpg" {
		t.Fatalf("first = %#v", songs[0])
	}
	if _, err := source.Chart(context.Background(), "rising", 1); err == nil {
		t.Fatal("an unknown chart must be refused")
	}
}

func TestQQChartReadsToplistRows(t *testing.T) {
	var asked string
	source := &qq{fetch: stub{
		"fcg_v8_toplist_cp.fcg": func(method, url string, body []byte) (int, []byte, error) {
			asked = url
			return 200, []byte(`{"songlist":[{"data":{"songmid":"m1","songname":"New",
				"albumname":"Al","albummid":"am","interval":228,"singer":[{"name":"S"}]}}]}`), nil
		},
	}.fetch}
	songs, err := source.Chart(context.Background(), "new", 30)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(asked, "topid=27") {
		t.Fatalf("new chart asked %q", asked)
	}
	if len(songs) != 1 || songs[0].ID != "m1" || songs[0].Title != "New" ||
		songs[0].Duration != 228 ||
		songs[0].Cover != "https://y.gtimg.cn/music/photo_new/T002R300x300M000am.jpg" {
		t.Fatalf("songs = %#v", songs)
	}
}

func TestKuwoChartReadsBangRows(t *testing.T) {
	var asked string
	source := &kuwo{fetch: stub{
		"/ksong.s": func(method, url string, body []byte) (int, []byte, error) {
			asked = url
			return 200, []byte(`{"musiclist":[
				{"id":"61","name":"Crazy&nbsp;Love","artist":"K","album":"Al","song_duration":"189"},
				{"id":"","name":"no id","artist":"","album":"","song_duration":"1"}]}`), nil
		},
	}.fetch}
	songs, err := source.Chart(context.Background(), "hot", 30)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(asked, "id=16") || !strings.Contains(asked, "rn=30") {
		t.Fatalf("hot chart asked %q", asked)
	}
	if len(songs) != 1 || songs[0].ID != "61" || songs[0].Title != "Crazy Love" ||
		songs[0].Duration != 189 || songs[0].Source != "kuwo" {
		t.Fatalf("songs = %#v", songs)
	}
}

func TestKuwoStreamAndLyric(t *testing.T) {
	source := &kuwo{fetch: stub{
		"/anti.s":         fixed(200, " http://kw-er.kuwo.cn/a.mp3 \n"),
		"/songinfoandlrc": fixed(200, `{"data":{"lrclist":[{"lineLyric":"b","time":"65.5"},{"lineLyric":"a","time":"0.0"}]}}`),
	}.fetch}
	stream, err := source.Stream(context.Background(), Song{ID: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if stream.URL != "https://kw-er.kuwo.cn/a.mp3" {
		t.Fatalf("stream = %#v", stream)
	}
	lyric, err := source.Lyric(context.Background(), Song{ID: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if lyric != "[00:00.00]a\n[01:05.50]b" {
		t.Fatalf("lyric = %q", lyric)
	}

	refused := &kuwo{fetch: stub{
		"/anti.s": fixed(200, "请使用酷我音乐客户端播放"),
	}.fetch}
	if _, err := refused.Stream(context.Background(), Song{ID: "1"}); err == nil {
		t.Fatal("a non-address answer must be a readable error")
	}
}

func TestFallbackSearchCarriesTheAggregatorIDsAndNetEaseCovers(t *testing.T) {
	asked := ""
	source := NewFallback(stub{
		"types=search": func(_, address string, _ []byte) (int, []byte, error) {
			asked = address
			return 200, []byte(`[{"id":"5257138","name":"屋顶","artist":["周杰伦","温岚"],
				"album":"男女情歌对唱冠军全记录","pic_id":"1","url_id":"9001","lyric_id":"9002","source":"netease"},
				{"id":"2","name":"No ids","artist":[],"album":""}]`), nil
		},
		"/api/song/detail": fixed(200, `{"songs":[{"id":5257138,"album":{"picUrl":"http://p2.music.126.net/a.jpg"}}]}`),
	}.fetch)
	songs, err := source.Search(context.Background(), "屋顶", 30)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(asked, "types=search") || !strings.Contains(asked, "source=netease") {
		t.Fatalf("search address = %q", asked)
	}
	if len(songs) != 2 {
		t.Fatalf("songs = %#v", songs)
	}
	first := songs[0]
	if first.Source != "netease" || first.Via != ViaFallback || first.ID != "5257138" {
		t.Fatalf("first = %#v", first)
	}
	if first.StreamID != "9001" || first.LyricID != "9002" {
		t.Fatalf("ids = %#v", first)
	}
	if first.Artist != "周杰伦 / 温岚" || first.Cover != "https://p2.music.126.net/a.jpg" {
		t.Fatalf("first = %#v", first)
	}
	// A row without a url id falls back to its own id, so it stays playable.
	if songs[1].StreamID != "2" || songs[1].LyricID != "2" || songs[1].Via != ViaFallback {
		t.Fatalf("second = %#v", songs[1])
	}
}

func TestFallbackResolvesStreamAndLyricThroughTheAggregator(t *testing.T) {
	asked := map[string]string{}
	source := NewFallback(stub{
		"types=url": func(_, address string, _ []byte) (int, []byte, error) {
			asked["url"] = address
			return 200, []byte(`{"url":"http://m801.music.126.net/a.mp3","br":320}`), nil
		},
		"types=lyric": func(_, address string, _ []byte) (int, []byte, error) {
			asked["lyric"] = address
			return 200, []byte(`{"lyric":"[00:00.00] 第一句\n[00:03.00] 第二句"}`), nil
		},
	}.fetch)
	song := Song{ID: "5257138", Via: ViaFallback, StreamID: "9001", LyricID: "9002"}
	stream, err := source.Stream(context.Background(), song)
	if err != nil {
		t.Fatal(err)
	}
	if stream.URL != "https://m801.music.126.net/a.mp3" || stream.Quality != "320 kbps" {
		t.Fatalf("stream = %#v", stream)
	}
	if !strings.Contains(asked["url"], "id=9001") || !strings.Contains(asked["url"], "br=320") {
		t.Fatalf("url address = %q", asked["url"])
	}
	lyric, err := source.Lyric(context.Background(), song)
	if err != nil {
		t.Fatal(err)
	}
	if lyric != "[00:00.00] 第一句\n[00:03.00] 第二句" {
		t.Fatalf("lyric = %q", lyric)
	}
	if !strings.Contains(asked["lyric"], "id=9002") {
		t.Fatalf("lyric address = %q", asked["lyric"])
	}
}

func TestFallbackRefusesAnEmptyAddress(t *testing.T) {
	source := NewFallback(stub{
		"types=url": fixed(200, `{"url":"","br":0}`),
	}.fetch)
	if _, err := source.Stream(context.Background(), Song{ID: "1"}); err == nil {
		t.Fatal("an empty address was accepted")
	}
}

func TestRescuePicksTheRowWithTheSameArtistAndResolvesIt(t *testing.T) {
	asked := ""
	source := NewFallback(stub{
		"types=search": func(_, address string, _ []byte) (int, []byte, error) {
			asked = address
			return 200, []byte(`[
				{"id":"1","name":"屋顶","artist":["另一个歌手"],"url_id":"11","lyric_id":"12"},
				{"id":"5257138","name":"屋顶 (Live)","artist":["周杰伦","温岚"],
				 "url_id":"9001","lyric_id":"9002","source":"netease"}]`), nil
		},
		"/api/song/detail": fixed(200, `{"songs":[{"id":1,"album":{"picUrl":""}},
			{"id":5257138,"album":{"picUrl":"http://p2.music.126.net/a.jpg"}}]}`),
		"types=url": fixed(200, `{"url":"http://m801.music.126.net/a.mp3","br":320}`),
	}.fetch)
	matched, stream, err := source.Rescue(context.Background(),
		Song{Source: "qq", ID: "qq-9", Title: "屋顶", Artist: "周杰伦 / 温岚"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(asked, "name=%E5%B1%8B%E9%A1%B6") {
		t.Fatalf("rescue searched %q", asked)
	}
	if matched.ID != "5257138" || matched.Via != ViaFallback || matched.StreamID != "9001" {
		t.Fatalf("matched = %#v", matched)
	}
	if matched.Cover != "https://p2.music.126.net/a.jpg" {
		t.Fatalf("cover = %q", matched.Cover)
	}
	if stream.URL != "https://m801.music.126.net/a.mp3" {
		t.Fatalf("stream = %#v", stream)
	}
}

func TestRescueLeavesAnotherSongAlone(t *testing.T) {
	resolved := false
	source := NewFallback(stub{
		"types=search": fixed(200, `[{"id":"7","name":"晴天","artist":["周杰伦"]}]`),
		"types=url": func(_, _ string, _ []byte) (int, []byte, error) {
			resolved = true
			return 200, []byte(`{"url":"http://m801.music.126.net/b.mp3","br":320}`), nil
		},
	}.fetch)
	if _, _, err := source.Rescue(context.Background(),
		Song{ID: "1", Title: "屋顶", Artist: "温岚"}); err == nil {
		t.Fatal("a different song was accepted")
	}
	if resolved {
		t.Fatal("an address was resolved for a different song")
	}
}
