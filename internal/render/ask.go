package render

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Question is what the server asks the person before a write that
// cannot be undone, or that sends something past the people who can
// already see it (§9.2). Text is the message a client shows; accepting
// it is the confirmation. Every word is the server's, except what stands
// in backticks, which is quoted from Favro or from the call and cut to
// one line. A blank line separates the lines, so a client that draws
// Markdown keeps them apart.
//
// Bind is what an answer is bound to: what the write depends on, which
// must not change between the question and the write. It is Text, and
// more where Text shows less than the write uses — an id, a file's
// whole content.
type Question struct {
	Text string
	Bind string
}

// quotedLen caps one quoted value: a name, a path, an address. A
// comment is capped at bodyLen.
const (
	quotedLen = 120
	bodyLen   = 300
)

// noUndo closes every question about a delete: Favro has none.
const noUndo = "Favro has no undo, so there is no way back."

// askDelete is the question before a delete Favro cannot undo: the tool,
// what it deletes and its name, where from, and what else goes.
func askDelete(tool, kind, name, where, consequence, id string) Question {
	return ask([]string{
		fmt.Sprintf("%s: delete the %s %s%s?", tool, kind, quoted(name, quotedLen), where),
		consequence,
		noUndo,
	}, id)
}

// AskDeleteTag asks before favro_delete_tag.
func AskDeleteTag(id, name string) Question {
	return askDelete("favro_delete_tag", "tag", name, " from the whole organization",
		"It comes off every card that carries it.", id)
}

// AskDeleteCollection asks before favro_delete_collection.
func AskDeleteCollection(id, name string) Question {
	return askDelete("favro_delete_collection", "collection", name, "",
		"Its widgets are not deleted with it, and one that was only in this collection may be left where nobody finds it.", id)
}

// AskDeleteWidget asks before favro_delete_widget. collection is the
// collection's name when the call names one, and empty when the widget
// goes from every collection it is in.
func AskDeleteWidget(id, name, collectionID, collection string) Question {
	head := fmt.Sprintf("favro_delete_widget: delete the widget %s from every collection it is in?", quoted(name, quotedLen))
	if collectionID != "" {
		head = fmt.Sprintf("favro_delete_widget: delete the widget %s from the collection %s?",
			quoted(name, quotedLen), quoted(collection, quotedLen))
	}
	return ask([]string{head, "The cards on it are removed, and its columns can no longer be reached.", noUndo},
		id, collectionID)
}

// AskDeleteGroup asks before favro_delete_group.
func AskDeleteGroup(id, name string) Question {
	return askDelete("favro_delete_group", "group", name, "",
		"It is taken off every share, assignment and custom field that names it.", id)
}

// AskDeleteWebhook asks before favro_delete_webhook. address is where
// the webhook posts, as Favro holds it.
func AskDeleteWebhook(id, name, address string) Question {
	return askDelete("favro_delete_webhook", "webhook", name, "",
		"It posts card events to "+quoted(address, quotedLen)+", and whatever receives them stops getting them.", id)
}

// AskDeleteCardEverywhere asks before a favro_delete_card that removes
// the card from every widget it is on.
func AskDeleteCardEverywhere(id, name string) Question {
	return askDelete("favro_delete_card", "card", name, " from every widget it is on",
		"Every copy goes, with its comments and attachments.", id)
}

// Upload is a file about to leave this computer for Favro.
type Upload struct {
	// Tool is the upload tool's name.
	Tool string
	// Path is the file's path inside FAVRO_UPLOAD_DIR, and Size its
	// length in bytes. Sum is Sum of its content: the question binds
	// the bytes, so a file changed while the person reads is not sent.
	Path string
	Size int64
	Sum  string
	// Filename is the name it gets in Favro.
	Filename string
	// TargetID is the card or comment it is attached to. OnComment is
	// true for a comment. Card is the card's name, for a card upload;
	// Comment is the comment's text, for a comment upload.
	TargetID      string
	OnComment     bool
	Card, Comment string
}

// AskUpload asks before an upload tool reads a file and sends it.
func AskUpload(u Upload) Question {
	target := "the card " + quoted(u.Card, quotedLen)
	if u.OnComment {
		target = "a comment"
	}
	lines := []string{
		fmt.Sprintf("%s: send the file %s (%d %s) from this computer to Favro, attached to %s?",
			u.Tool, quoted(u.Path, quotedLen), u.Size, Plural(int(u.Size), "byte", "bytes"), target),
	}
	if u.OnComment {
		lines = append(lines, body0(u.Comment))
	}
	if u.Filename != "" && u.Filename != path.Base(u.Path) {
		lines = append(lines, "It is named "+quoted(u.Filename, quotedLen)+" there.")
	}
	lines = append(lines, "Everyone who can see it in Favro can open the file.")
	return ask(lines, u.TargetID, u.Sum, Sum(u.Comment))
}

// AskPublicCollection asks before a collection is made public. name is
// the collection's name; created is true for a new one.
func AskPublicCollection(tool, id, name string, created bool) Question {
	head := fmt.Sprintf("%s: make the collection %s public?", tool, quoted(name, quotedLen))
	if created {
		head = fmt.Sprintf("%s: create the collection %s as public?", tool, quoted(name, quotedLen))
	}
	return ask([]string{head, "Anyone on the internet with its link can see it, without signing in to Favro."}, id)
}

// body0 is the start of a body, quoted on one line, and how much more
// there is.
func body0(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return "text: empty"
	}
	line := "text: " + quoted(body, bodyLen)
	if n := utf8.RuneCountInString(body); n > bodyLen {
		line += fmt.Sprintf(" (%d more characters)", n-bodyLen)
	}
	return line
}

// Sum is a sha256 digest in hex: how an answer is bound to a whole text, of
// which a question shows only the start.
func Sum(s string) string { return SumBytes([]byte(s)) }

// SumBytes is Sum of b, without copying it to a string first.
func SumBytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ask builds a question from its lines, closes it with what its quotes
// mean, sets its lines apart, and binds it to its text and to bind: the
// ids and whole texts the write depends on beyond what it shows.
func ask(lines []string, bind ...string) Question {
	text := strings.Join(lines, "\n")
	if strings.Contains(text, "`") {
		text += "\nText in backticks or code style is quoted as written, and is not this server's."
	}
	text = strings.ReplaceAll(text, "\n", "\n\n") + "\n"
	return Question{Text: text, Bind: strings.Join(append([]string{text}, bind...), "\x00")}
}

// quoted is text from Favro or from a call's arguments, shown in a
// question put to the person, where no boundary can go: a client draws
// the question as plain text in a dialog, or as Markdown. It stands in
// a code span, `like this`, which Markdown shows literally — no
// emphasis, link, HTML or entity — and plain text shows as it is. It is
// made one line; every backtick, grave or acute mark and quote mark a
// reader could take for one becomes a plain single quote, so it cannot
// close its span or seem to; and a URL scheme, a mailto:, a leading
// "www." and a bare domain followed by a path are broken so no client
// draws a link. It is cut at limit runes. Text with nothing to show is
// said in words, since an empty span is two backticks Markdown shows as
// they are: "empty" when it is blank, and "invisible characters only"
// when it is not.
func quoted(s string, limit int) string {
	blank := strings.TrimSpace(s) == ""
	s = strings.Join(strings.Fields(blankMarks.Replace(askLine(s, limit))), " ")
	s = quoteMarks.Replace(s)
	s = linkShape.ReplaceAllString(s, "${1}[:]//")
	s = mailtoShape.ReplaceAllString(s, "${1}[:]")
	s = wwwShape.ReplaceAllString(s, "${1}[.]")
	s = pathShape.ReplaceAllString(s, "${1}[.]${2}${3}")
	switch {
	case s == "" && blank:
		return "empty"
	case s == "":
		return "invisible characters only"
	}
	return "`" + s + "`"
}

// askLine is text made one line: format characters, which draw nothing
// and can reorder what does, removed; controls and line separators made
// spaces; cut at limit runes.
func askLine(s string, limit int) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.In(r, unicode.Cf, unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point):
			return -1
		case unicode.IsControl(r), r == ' ', r == ' ':
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, "�"))
	if utf8.RuneCountInString(s) > limit {
		s = string([]rune(s)[:limit]) + ellipsis
	}
	return s
}

var (
	// quoteMarks folds every backtick, grave or acute mark and quotation
	// mark a reader could take for the question's own to a plain single
	// quote.
	quoteMarks = strings.NewReplacer("`", "'", "ˋ", "'", "｀", "'", "`", "'", "´", "'",
		"ˊ", "'", "˴", "'", "˵", "'", "´", "'", "῭", "'", "΅", "'",
		"΄", "'", "΅", "'", `"`, "'", "‘", "'", "’", "'", "‚", "'", "‛", "'",
		"“", "'", "”", "'", "„", "'", "‟", "'", "′", "'", "″", "'",
		"«", "'", "»", "'", "‹", "'", "›", "'", "〝", "'", "〞", "'",
		"〟", "'", "＂", "'", "＇", "'", "ʹ", "'", "ʺ", "'", "ˮ", "'",
		"׳", "'", "״", "'", "‵", "'", "‶", "'", "❛", "'", "❜", "'",
		"❝", "'", "❞", "'", "〃", "'")
	// blankMarks are characters drawn as blank space that are not format
	// characters; they become spaces and collapse with the rest.
	blankMarks = strings.NewReplacer("⠀", " ", "ㅤ", " ", "ﾠ", " ", "ᅟ", " ", "ᅠ", " ")
	// No shape is anchored: \b is ASCII-only, and a class before the
	// shape would consume a separator the next link needs. A match inside
	// a longer word is broken too, which costs only a bracket.
	//
	// linkShape is a URL scheme followed by //, as a client links it.
	linkShape = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*)://`)
	// mailtoShape is a mail link without //.
	mailtoShape = regexp.MustCompile(`(?i)(mailto):`)
	// wwwShape is a host a client links without a scheme.
	wwwShape = regexp.MustCompile(`(?i)(www)\.`)
	// pathShape is a bare domain followed by a path, a port, a query or a
	// fragment, x.example/..., which a client links too; its last dot is
	// broken. Letters and their marks from any script count, so a
	// non-ASCII domain is broken as well.
	pathShape = regexp.MustCompile(`(?i)([\p{L}\p{M}\p{N}-]+(?:\.[\p{L}\p{M}\p{N}-]+)*)\.([\p{L}\p{M}]{2,63})([/:?#])`)
)
