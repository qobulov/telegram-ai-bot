// Package quizfile reads test questions written the way Uzbek universities
// write them (the HEMIS format): a question, then its options each after a
// line of "====", the right one marked with "#", and "++++" before the
// next question.
//
//	Ishlab chiqarishning qanday turlari mavjud?
//	====
//	#moddiy va nomoddiy
//	====
//	barter va tovar
//	++++
//
// The markers are found anywhere, not only on lines of their own, because
// text taken out of a PDF often loses its line breaks.
package quizfile

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Question is one test question with its options; Correct is the index of
// the right one.
type Question struct {
	Text    string   `json:"q"`
	Options []string `json:"o"`
	Correct int      `json:"c"`
}

// Limits on what is taken as a question, after Telegram's: a quiz has 2 to
// 10 options. Longer texts are kept; showing them is the sender's concern.
const (
	MinOptions = 2
	MaxOptions = 10
	// maxQuestionRunes bounds a question; a longer first block is the
	// title page in front of the first question.
	maxQuestionRunes = 1000
	// maxFirstRunes is past which the first block is taken for a title
	// page with the question on its last line.
	maxFirstRunes = 300
)

var (
	questionMark = regexp.MustCompile(`\+{4,}`)
	optionMark   = regexp.MustCompile(`={4,}`)
	spaces       = regexp.MustCompile(`\s+`)
)

// Parse reads the questions in text, without repeats, and counts the
// blocks that looked like questions but could not be read: no right answer
// marked, more than one, or too few options. A test that gives only the
// right answers is read too, and madeOptions reports that its wrong
// options were taken from the other answers.
func Parse(text string) (questions []Question, broken int, madeOptions bool) {
	questions, broken = parseOptions(text)
	if answered, unread := parseAnswers(text); len(answered) > len(questions) {
		return answered, unread, true
	}
	return questions, broken, false
}

// parseOptions reads a test whose questions have their options, the right
// one marked.
func parseOptions(text string) (questions []Question, broken int) {
	seen := map[string]bool{}
	for i, block := range splitQuestions(text) {
		q, ok := parseBlock(block, i == 0)
		if !ok {
			if optionMark.MatchString(block) {
				broken++
			}
			continue
		}
		key := q.Text + "\x00" + q.Options[q.Correct]
		if seen[key] {
			continue
		}
		seen[key] = true
		questions = append(questions, q)
	}
	return questions, broken
}

// splitQuestions cuts text at "++++", and also where a "++++" was left out:
// an option followed, on a line of its own, by a line ending in "?" or ":"
// is taken to run into the next question.
func splitQuestions(text string) []string {
	var blocks []string
	for _, block := range questionMark.Split(text, -1) {
		blocks = append(blocks, splitMissingMarks(block)...)
	}
	return blocks
}

func splitMissingMarks(block string) []string {
	parts := optionMark.Split(block, -1)
	if len(parts) < 3 {
		return []string{block}
	}
	// Any option but the last may end with the next question's text.
	for i := 1; i < len(parts)-1; i++ {
		option, question, ok := cutTrailingQuestion(parts[i])
		if !ok {
			continue
		}
		first := strings.Join(append(append([]string{}, parts[:i]...), option), "====")
		rest := question + "====" + strings.Join(parts[i+1:], "====")
		return append([]string{first}, splitMissingMarks(rest)...)
	}
	return []string{block}
}

// cutTrailingQuestion splits an option whose last line reads as a question:
// it ends with "?" or ":" and is not the option's only line.
func cutTrailingQuestion(option string) (string, string, bool) {
	lines := nonEmptyLines(option)
	if len(lines) < 2 {
		return "", "", false
	}
	last := lines[len(lines)-1]
	if !strings.HasSuffix(last, "?") && !strings.HasSuffix(last, ":") {
		return "", "", false
	}
	return strings.Join(lines[:len(lines)-1], "\n"), last, true
}

func nonEmptyLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// parseBlock reads one question. The first block may start with the
// file's title page; its question is then only its last line.
func parseBlock(block string, first bool) (Question, bool) {
	parts := optionMark.Split(block, -1)
	if len(parts) < 1+MinOptions {
		return Question{}, false
	}
	text := clean(parts[0])
	limit := maxQuestionRunes
	if first {
		limit = maxFirstRunes
	}
	if lines := nonEmptyLines(parts[0]); len(lines) > 1 && utf8.RuneCountInString(text) > limit {
		text = clean(lines[len(lines)-1])
	}
	if text == "" || utf8.RuneCountInString(text) > maxQuestionRunes {
		return Question{}, false
	}
	q := Question{Text: text, Correct: -1}
	for _, part := range parts[1:] {
		option := clean(part)
		if option == "" {
			continue
		}
		if strings.HasPrefix(option, "#") {
			if q.Correct >= 0 {
				return Question{}, false
			}
			q.Correct = len(q.Options)
			option = strings.TrimSpace(strings.TrimPrefix(option, "#"))
		}
		q.Options = append(q.Options, option)
	}
	if q.Correct < 0 || len(q.Options) < MinOptions || len(q.Options) > MaxOptions {
		return Question{}, false
	}
	return q, true
}

// clean is text on one line, its spaces collapsed.
func clean(text string) string {
	return strings.TrimSpace(spaces.ReplaceAllString(text, " "))
}
