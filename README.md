# 音乐播放器

MyPaste extension `com.mypaste.music` (version `1.3.3`).

设置窗口的一级菜单「音乐」。左栏是搜索结果、热歌榜、新歌榜、收藏与队列几栏列表，搜索框与平台选择（网易云音乐、QQ 音乐、酷我）同一行；「搜索」栏记住最近 5 次搜索，没有结果时落在它们上面，点一行重跑、点叉删掉一条、点「清除」清空整块，切去看榜单或队列再回来，关键词与结果都还在。右栏是随时钟滚动、点一行即可跳转的歌词；有歌在播时整页变成居中的唱片、歌名与歌词，列表收进底栏按钮后面。页面不铺自己的底色，封面只出现在播放条与列表行里，整页落在宿主解析出的两种表面色上；底栏的随机与循环是两个形状——交织的箭头与一枚闭环，单曲循环在环里写一个 1；窗口窄成侧边栏时同一时刻只给一个视图——列表开着就占满整帧，收起就交给播放器与歌词。平台相关的一切都在扩展自己的进程里：搜索、榜单、播放地址与歌词只经宿主的 `network.outbound` 代理访问 manifest 声明的八个主机，歌曲与封面由页面直接从平台给出的地址加载；收藏、搜索记录与偏好存在扩展私有目录，卸载即随之删除。

The page draws the player: the result / hot-chart / new-chart / favourites / queue rail with a ranked 热歌榜 and 新歌榜 read from the selected platform, and an immersive lyric sheet that follows the clock and seeks when a line is clicked. The search field lives in the search tab only, beside the platform picker (NetEase Cloud Music, QQ Music, Kuwo) that also scopes the charts; `/` from anywhere brings that tab and the caret up. The tab keeps the last five queries, newest first and a repeat moved up rather than doubled, and opens on them when it has no results to show. They survive a restart in the runtime's state, and leaving the tab for a chart or the queue and coming back keeps the keyword in the field and its rows under it. The page claims no colour of its own: the playing cover shows in the now block and the rows, never as a wash behind them, and the list column, cards, fields and transport sit on the host's two surface colours. Shuffle and repeat are two shapes rather than one drawn twice: the crossed arrows and a closed loop, with repeat-one carrying a numeral inside the loop. Below 760 px one view owns the frame at a time — the list while it is up, the player and its lyrics while it is not — and below 520 px the now bar gives its title a row of its own. Once a song is on, the page is the player and its lyrics: picking a row folds the list away, the sheet centres on the album — a 168px sleeve, the title and artist under it, the lyric lines centred beneath — and the mini bar's list button (or `/`, or Escape) brings the list back.

Nothing about the platforms lives in the page: `music.request` searches, reads a chart, resolves a playable address and reads the lyric in the compiled `music` binary, which reaches the platforms only through the host's `network.outbound` proxy and only the eight hosts its manifest declares; requests carry no cookie, referer or user agent beyond what the proxy itself sends. A search the platform answers with nothing — or with an error — is retried once through the GD Studio aggregator (`music-api.gdstudio.xyz`), whose rows stay NetEase songs with their own stream and lyric ids and their album art filled from NetEase's detail endpoint. A platform that cannot answer a playable address — a member-only track, a region block, a row that is gone — is rescued the same way: the aggregator is searched by the song's own title and artist, the row naming the same track is resolved, and the page adopts that row so the cover, lyric and queue follow what is playing. A switch is one transaction: until the new address actually plays, the previous song, queue, index, lyric and audio element stay exactly as they were. Audio and cover bytes never pass through the runtime — the page loads the addresses it resolves — and no secret, entity, clipboard or history service is used.

The saved document — volume, source, favourites, recently played, the five searches, repeat and shuffle — lives behind `storage.plugin` and is normalised by the runtime on both read and write: a known source, a bounded volume, a known repeat mode and bounded lists, so a hand-edited file never reaches the page unchanged. The page never sees the stored file, the filesystem or the network outside the addresses the runtime resolves.

This repository is the extension's own source home. It contains only this extension's tracked source files. The published `.mypaste-plugin` archives and the marketplace index live in [`mypaste-extensions-catalog`](https://github.com/mypaste/mypaste-extensions-catalog).

The manifest speaks MyPaste plugin protocol v3. Installation, capability mediation, and content-digest verification are performed by the MyPaste desktop host.

## Files

- `mypaste.plugin.json`: package identity, the 「音乐」 menu entry, the placement-less `music.request` command, the three capabilities (`network.outbound`, `storage.plugin`, `surface.network`) and the eight declared hosts.
- `panel.html`: the menu page — the five-tab list, the lyric sheet, the transport, the host's theme tokens, and every platform request routed through the host.
- `src/`: the extension's own Go process — `internal/music` (NetEase, QQ, Kuwo and the GD Studio fallback: search, charts, stream and lyric over the host's proxy), `internal/host` (the line protocol and the storage/network calls), and `cmd/mypaste-music` (request dispatch and state normalisation).

## Build

```bash
cd src && CGO_ENABLED=0 go build -trimpath ./cmd/mypaste-music
```

The released archive carries the `music` binary beside the manifest (darwin arm64 and amd64) plus the page.

## License

MIT. See [LICENSE](LICENSE).
