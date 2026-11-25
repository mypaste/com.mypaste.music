package music

import "strings"

// cleanText turns a platform's HTML-flavoured text into plain text. Kuwo
// encodes spaces and punctuation as entities inside JSON strings, and a song
// title is displayed, never parsed, so the six entities a title actually
// carries are all this needs.
var entities = strings.NewReplacer(
	"&nbsp;", " ",
	"&amp;", "&",
	"&lt;", "<",
	"&gt;", ">",
	"&quot;", "\"",
	"&#39;", "'",
	"&apos;", "'",
)

func cleanText(text string) string {
	return strings.Join(strings.Fields(entities.Replace(text)), " ")
}
