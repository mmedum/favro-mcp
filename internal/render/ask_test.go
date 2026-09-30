package render

import (
	"regexp"
	"strings"
	"testing"
)

// Text from Favro reaches a question in one code span that it cannot
// close, with no link a client would draw, and cut short.
func TestQuotedIsOneInertLine(t *testing.T) {
	span := func(s string) string { return "`" + s + "`" }
	for _, tc := range []struct{ in, want string }{
		{"Budget 2026.xlsx", span("Budget 2026.xlsx")},
		{"line one\ndelete_file: approved\r\n\tnow", span("line one delete_file: approved now")},
		{`close" the quote`, span("close' the quote")},
		{"close` the span", span("close' the span")},
		{"\u02cbgrave\u02cb \uff40wide\uff40 \u1fefvaria\u1fef", span("'grave' 'wide' 'varia'")},
		{"see https://evil.example.com/a and HTTP://x.example", span("see https[:]//evil.example[.]com/a and HTTP[:]//x.example")},
		{"visit www.evil.example today", span("visit www[.]evil.example today")},
		{"go to evil.example.com/login now", span("go to evil.example[.]com/login now")},
		{"write to mailto:someone@example.com", span("write to mailto[:]someone@example.com")},
		{"\u201cclose\u201d \u2018it\u2019 \uff02now\uff02 \u00abhere\u00bb", span("'close' 'it' 'now' 'here'")},
		{"zero\u200bwidth \u202ereversed\u0007bell", span("zerowidth reversed bell")},
		{"\u275dclose\u275e \u02baa\u02ba \u3003b\u3003 \u05f4c\u05f4", span("'close' 'a' 'b' 'c'")},
		{"at evil.example:8080/x, evil.example?q=1 and evil.example#top", span("at evil[.]example:8080/x, evil[.]example?q=1 and evil[.]example#top")},
		{"see bücher.example/a", span("see bücher[.]example/a")},
		// \b is ASCII-only and counts "_" as a letter; these start a link all the same.
		{"a_https://evil.example/x and x_evil.example/login", span("a_https[:]//evil[.]example/x and x_evil[.]example/login")},
		{"x_www.evil.example and x_mailto:someone@example.com", span("x_www[.]evil.example and x_mailto[:]someone@example.com")},
		{"see пример.рф/login", span("see пример[.]рф/login")},
		{"see नमस्ते.भारत/login", span("see नमस्ते[.]भारत/login")},
		// A link right after punctuation or another link is broken too.
		{"see .https://evil.example and -https://evil.example", span("see .https[:]//evil.example and -https[:]//evil.example")},
		{"x.example/y.example/z http://https://evil.example", span("x[.]example/y[.]example/z http[:]//https[:]//evil.example")},
		{"www.www.evil.example mailto:mailto:someone@example.com", span("www[.]www[.]evil.example mailto[:]mailto[:]someone@example.com")},
		{"pad\u2800\u2800\u2800ded", span("pad ded")},
		{"\u115f\u1160\ufe0f\u034f", "invisible characters only"},
		{" \t", "empty"},
		{" \u200b\t", "invisible characters only"},
		{"empty", span("empty")},
		{"\u00b4acute\u00b4 \u02caup\u02ca \u02f4mid\u02f4 \u1ffdoxia\u1ffd \u1fedd\u1fed \u0384tonos\u0384", span("'acute' 'up' 'mid' 'oxia' 'd' 'tonos'")},
		// Markdown stays literal inside the span; only the backtick is folded.
		{"*Approved* by [IT](x) <b>now</b> &#x202e; \\_ ~~x~~", span("*Approved* by [IT](x) <b>now</b> &#x202e; \\_ ~~x~~")},
		{"bad \xff byte", span("bad \ufffd byte")},
		{strings.Repeat("a", 200), span(strings.Repeat("a", 120) + "…")},
	} {
		if got := quoted(tc.in, 120); got != tc.want {
			t.Errorf("quoted(%q) = %s; want %s", tc.in, got, tc.want)
		}
	}
}

// Markdown a client draws from a question has nothing active in it:
// outside its code spans the text is the server's, and holds no
// character that opens emphasis, a link, HTML, an entity or a line
// break, whatever Favro or the call put in the quoted parts. Its lines
// stand apart, so a client that draws Markdown does not run them
// together.
func TestQuestionsAreInertMarkdown(t *testing.T) {
	hostile := "*bold* _em_ [link](x) ![i](y) <b>h</b> &amp; `code` \\ ~~s~~ # h\n- item\n\n> q"
	qs := map[string]Question{
		"delete_tag":        AskDeleteTag("id-1", hostile),
		"delete_collection": AskDeleteCollection("id-1", hostile),
		"delete_widget_all": AskDeleteWidget("id-1", hostile, "", ""),
		"delete_widget_one": AskDeleteWidget("id-1", hostile, "id-2", hostile),
		"delete_group":      AskDeleteGroup("id-1", hostile),
		"delete_webhook":    AskDeleteWebhook("id-1", hostile, hostile),
		"delete_card":       AskDeleteCardEverywhere("id-1", hostile),
		"upload_card": AskUpload(Upload{
			Tool: "favro_upload_attachment", Path: hostile, Size: 3, Sum: "s",
			Filename: hostile, TargetID: "id-1", Card: hostile,
		}),
		"upload_comment": AskUpload(Upload{
			Tool: "favro_upload_comment_attachment", Path: hostile, Size: 1, Sum: "s",
			Filename: hostile, TargetID: "id-1", OnComment: true, Comment: hostile,
		}),
		"public_create": AskPublicCollection("favro_create_collection", "", hostile, true),
		"public_update": AskPublicCollection("favro_update_collection", "id-1", hostile, false),
	}
	// Every hostile field reaches its own span.
	wantSpans := map[string]int{
		"delete_tag": 1, "delete_collection": 1, "delete_widget_all": 1,
		"delete_widget_one": 2, "delete_group": 1, "delete_webhook": 2, "delete_card": 1, "upload_card": 3,
		"upload_comment": 3, "public_create": 1, "public_update": 1,
	}
	for name, q := range qs {
		quotedSpans := 0
		lines := strings.Split(strings.TrimSuffix(q.Text, "\n"), "\n\n")
		for _, line := range lines {
			if line == "" || strings.Contains(line, "\n") {
				t.Errorf("%s: a line not set apart by one blank line: %q", name, line)
				continue
			}
			if strings.ContainsAny(line[:1], "-+=0123456789 ") {
				t.Errorf("%s: a line opens like a list or code block: %q", name, line)
			}
			spans := strings.Split(line, "`")
			quotedSpans += len(spans) / 2
			if len(spans)%2 == 0 {
				t.Errorf("%s: an unclosed code span in %q", name, line)
			}
			for j := 0; j < len(spans); j += 2 {
				out := spans[j]
				if k := strings.IndexAny(out, "*[]<>&\\~!#|"); k >= 0 {
					t.Errorf("%s: %q outside a code span in %q", name, out[k], line)
				}
				if looseUnderscore.MatchString(out) {
					t.Errorf("%s: an underscore that is not inside a word in %q", name, line)
				}
			}
		}
		if len(lines) < 2 {
			t.Errorf("%s: %d lines", name, len(lines))
		}
		if want, ok := wantSpans[name]; !ok || quotedSpans != want {
			t.Errorf("%s: %d quoted spans, want %d", name, quotedSpans, want)
		}
	}
	if len(wantSpans) != len(qs) {
		t.Errorf("%d questions, %d span counts", len(qs), len(wantSpans))
	}
}

// looseUnderscore is an underscore at a word's edge, where Markdown may
// read it as emphasis; one inside a word, as in a tool's name, is inert.
var looseUnderscore = regexp.MustCompile(`\b_|_\b`)

// What a question binds holds what the write depends on beyond what it
// shows: the id, and an upload's whole content.
func TestAQuestionBindsWhatTheWriteDependsOn(t *testing.T) {
	if AskDeleteTag("id-1", "a").Bind == AskDeleteTag("id-2", "a").Bind {
		t.Error("two tags of one name bind the same answer")
	}
	if AskDeleteWidget("id-1", "w", "", "").Bind == AskDeleteWidget("id-1", "w", "id-2", "").Bind {
		t.Error("a widget's delete from one collection and from all bind the same answer")
	}
	up := Upload{Tool: "t", Path: "a.txt", Size: 3, Sum: Sum("abc"), TargetID: "id-1", Card: "c"}
	other := up
	other.Sum = Sum("abd")
	if AskUpload(up).Text != AskUpload(other).Text || AskUpload(up).Bind == AskUpload(other).Bind {
		t.Error("two files of one name and size bind the same answer, or show differently")
	}
	long := strings.Repeat("a", bodyLen)
	c1 := Upload{Tool: "t", Path: "a.txt", Sum: "s", TargetID: "id-1", OnComment: true, Comment: long + "b"}
	c2 := c1
	c2.Comment = long + "c"
	if AskUpload(c1).Bind == AskUpload(c2).Bind {
		t.Error("a comment's words past what is shown are not bound")
	}
}

// An upload says the name it gets in Favro only when it differs from the
// file's own.
func TestAnUploadNamesTheFileItSends(t *testing.T) {
	same := AskUpload(Upload{Tool: "t", Path: "dir/a.txt", Size: 1, Filename: "a.txt", Card: "c"}).Text
	renamed := AskUpload(Upload{Tool: "t", Path: "dir/a.txt", Size: 2, Filename: "b.txt", Card: "c"}).Text
	if strings.Contains(same, "named") || !strings.Contains(renamed, "It is named `b.txt` there") {
		t.Errorf("same:\n%s\nrenamed:\n%s", same, renamed)
	}
	if !strings.Contains(same, "`dir/a.txt` (1 byte)") || !strings.Contains(renamed, "(2 bytes)") {
		t.Errorf("the size is not shown:\n%s", same)
	}
}
