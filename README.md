# 音乐播放器

MyPaste extension `com.mypaste.music` (version `1.4.0`).

设置窗口的一级菜单「音乐」。浏览是一面海报墙：搜索结果、按平台名次读出的热歌榜与新歌榜、收藏与队列，封面铺满每张方形卡片，歌名与歌手压在海报下沿的标签板上，正在播的那张用强调色描边、角标从名次换成跳动的播放条，指针或键盘聚焦时其余封面后退半步，墙够长时一张海报升成两倍大。搜索框与平台选择（网易云音乐、QQ 音乐、酷我）同一行；「搜索」栏记住最近 5 次搜索，没有结果时落在它们上面，点一行重跑、点叉删掉一条、点「清除」清空整块，切去看榜单或队列再回来，关键词与海报都还在。有歌在播时页面交给唱片条——左边 132px 的封面、右边放大的歌名、一条细线收住这条带——歌词在它左沿之下按 720px 的阅读宽度排开，点一行即可跳转；底栏列表按钮（或 /、或 Esc）把墙叫回来。页面不铺自己的底色，封面只出现在海报与播放条里，整页落在宿主解析出的两种表面色上；底栏的随机与循环是两个形状——交织的箭头与一枚闭环，单曲循环在环里写一个 1；窗口窄成侧边栏时顶部折成两行、封面退一档大小。平台相关的一切都在扩展自己的进程里：搜索、榜单、播放地址与歌词只经宿主的 `network.outbound` 代理访问 manifest 声明的八个主机，歌曲与封面由页面直接从平台给出的地址加载；收藏、搜索记录与偏好存在扩展私有目录，卸载即随之删除。

Browsing is a wall of square posters: search results, the ranked 热歌榜 and 新歌榜 read from the selected platform, favourites and the queue, each cover filling its card with the title and artist on a plate at its foot, the playing song framed in the accent with a playing chip where a chart's rank would be, the rest of the wall stepping back under the pointer or the keyboard's focus, and one poster lifted to twice the size on a long wall. The search field lives in the search tab only, beside the platform picker (NetEase Cloud Music, QQ Music, Kuwo) that also scopes the charts; `/` from anywhere brings that tab and the caret up. The tab keeps the last five queries, newest first and a repeat moved up rather than doubled, and opens on them when it has no results to show. They survive a restart in the runtime's state, and leaving the tab for a chart or the queue and coming back keeps the keyword in the field and its posters under it. The page claims no colour of its own: the playing cover shows on the poster and in the now strip, never as a wash behind anything; the wall, the label plates, the fields and the transport sit on the host's two surface colours. Shuffle and repeat are two shapes rather than one drawn twice: the crossed arrows and a closed loop, with repeat-one carrying a numeral inside the loop. Below 760 px the band folds into two rows and the covers drop a size. Once a song is on, the page is the player and its lyrics: picking a poster hands the frame to the record strip — a 132px sleeve at the left, the title set large beside it, a hairline closing the band — with the lyric lines reading on under its left edge on a 720px measure, and a click on a line seeks the clock there; the mini bar's list button (or `/`, or Escape) brings the wall back.

Nothing about the platforms lives in the page: `music.request` searches, reads a chart, resolves a playable address and reads the lyric in the compiled `music` binary, which reaches the platforms only through the host's `network.outbound` proxy and only the eight hosts its manifest declares; requests carry no cookie, referer or user agent beyond what the proxy itself sends. A search the platform answers with nothing — or with an error — is retried once through the GD Studio aggregator (`music-api.gdstudio.xyz`), whose rows stay NetEase songs with their own stream and lyric ids and their album art filled from NetEase's detail endpoint. A platform that cannot answer a playable address — a member-only track, a region block, a row that is gone — is rescued the same way: the aggregator is searched by the song's own title and artist, the row naming the same track is resolved, and the page adopts that row so the cover, lyric and queue follow what is playing. A switch is one transaction: until the new address actually plays, the previous song, queue, index, lyric and audio element stay exactly as they were. Audio and cover bytes never pass through the runtime — the page loads the addresses it resolves — and no secret, entity, clipboard or history service is used.

The saved document — volume, source, favourites, recently played, the five searches, repeat and shuffle — lives behind `storage.plugin` and is normalised by the runtime on both read and write: a known source, a bounded volume, a known repeat mode and bounded lists, so a hand-edited file never reaches the page unchanged. The page never sees the stored file, the filesystem or the network outside the addresses the runtime resolves.

This repository is the extension's own source home. It contains only this extension's tracked source files. The published `.mypaste-plugin` archives and the marketplace index live in [`mypaste-extensions-catalog`](https://github.com/mypaste/mypaste-extensions-catalog).

The manifest speaks MyPaste plugin protocol v3. Installation, capability mediation, and content-digest verification are performed by the MyPaste desktop host.

## Files

- `mypaste.plugin.json`: package identity, the 「音乐」 menu entry, the placement-less `music.request` command, the three capabilities (`network.outbound`, `storage.plugin`, `surface.network`) and the eight declared hosts.
- `panel.html`: the menu page — the poster wall, the record strip and its lyrics, the transport, the host's theme tokens, and every platform request routed through the host.
- `src/`: the extension's own Go process — `internal/music` (NetEase, QQ, Kuwo and the GD Studio fallback: search, charts, stream and lyric over the host's proxy), `internal/host` (the line protocol and the storage/network calls), and `cmd/mypaste-music` (request dispatch and state normalisation).

## Build

```bash
cd src && CGO_ENABLED=0 go build -trimpath ./cmd/mypaste-music
```

The released archive carries the `music` binary beside the manifest (darwin arm64 and amd64) plus the page.

## License

MIT. See [LICENSE](LICENSE).
