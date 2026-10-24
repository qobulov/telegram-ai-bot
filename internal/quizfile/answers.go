package quizfile

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Some test banks give each question only its right answer: the question,
// then "# answer", the questions apart by "=====" or "===". Where the "#" is
// left out, the answer is the line after the question's last line, the one
// ending in "?", "." or ":".
//
//	+++++
//	Bir butunni bo'laklarga bo'lib o'rganish –
//	# Analiz
//	=====
//	Rejada 20 kg, haqiqatda 25 kg. Farqini toping.
//	5 kg
//
// Such a test has no wrong options to choose from, so they are taken from
// the answers of the questions near it: a figure's from other figures, a
// term's from other terms, which are on the same topic.

const (
	// optionsMade is how many options a question with only its answer gets:
	// the answer and three others.
	optionsMade = 4
	// maxAnswerLines and maxAnswerRunes bound an answer found without "#",
	// so that a paragraph is not taken for one.
	maxAnswerLines = 3
	maxAnswerRunes = 300
	// maxFigureLetters is how many letters a figure or a formula may have,
	// for its units, as "mlrd so'm", or its names, as "Kml = (PM + QMI) / JM".
	maxFigureLetters = 15
)

var (
	answerBlockMark = regexp.MustCompile(`={3,}|\+{4,}`)
	answerMark      = regexp.MustCompile(`(?:^|\s)#`)
)

// answered is a question with its right answer only.
type answered struct {
	text, answer string
}

// parseAnswers reads a test of questions with their answers only, and
// gives each three wrong options from the other answers. It counts the
// blocks that could not be read: two answers marked, or none found.
func parseAnswers(text string) (questions []Question, broken int) {
	var read []answered
	seen := map[string]bool{}
	for i, block := range answerBlockMark.Split(text, -1) {
		if strings.TrimSpace(block) == "" {
			continue
		}
		a, ok := parseAnswered(block)
		if !ok {
			// The first block is the title page in front of the first
			// question.
			if i > 0 {
				broken++
			}
			continue
		}
		key := a.text + "\x00" + a.answer
		if !seen[key] {
			seen[key] = true
			read = append(read, a)
		}
	}
	if len(read) < optionsMade {
		return nil, broken
	}
	for i := range read {
		if q, ok := withOptions(read, i); ok {
			questions = append(questions, q)
		} else {
			broken++
		}
	}
	return questions, broken
}

// parseAnswered reads a question and its answer, marked by "#" or on the
// lines after the question.
func parseAnswered(block string) (answered, bool) {
	marks := answerMark.FindAllStringIndex(block, -1)
	switch len(marks) {
	case 0:
		return answerAfterQuestion(block)
	case 1:
		at := strings.Index(block[marks[0][0]:], "#") + marks[0][0]
		a := answered{text: clean(block[:at]), answer: clean(block[at+1:])}
		return a, a.fits()
	}
	return answered{}, false
}

// answerAfterQuestion takes as the answer the lines after the last one
// that ends a question.
func answerAfterQuestion(block string) (answered, bool) {
	lines := nonEmptyLines(block)
	for end := len(lines) - 2; end >= 0 && len(lines)-1-end <= maxAnswerLines; end-- {
		if strings.HasSuffix(lines[end], "?") || strings.HasSuffix(lines[end], ".") || strings.HasSuffix(lines[end], ":") {
			a := answered{text: clean(strings.Join(lines[:end+1], " ")), answer: clean(strings.Join(lines[end+1:], " "))}
			return a, a.fits()
		}
	}
	return answered{}, false
}

func (a answered) fits() bool {
	return a.text != "" && a.answer != "" && utf8.RuneCountInString(a.text) <= maxQuestionRunes &&
		utf8.RuneCountInString(a.answer) <= maxAnswerRunes
}

// withOptions is question i with its answer first and three others: the
// answers nearest to it of the same kind, figure, formula or words, that differ
// from it. Answers to the same question asked again are not taken: they
// may be right too, nor answers that hold it or that it holds, as
// "Daromadlar va xarajatlar, foyda" for "Daromadlar va xarajatlar".
func withOptions(read []answered, i int) (Question, bool) {
	q := Question{Text: read[i].text, Options: []string{read[i].answer}}
	taken := map[string]bool{fold(read[i].answer): true}
	kind := kindOf(read[i].answer)
	for _, sameKind := range []bool{true, false} {
		for distance := 1; distance < len(read) && len(q.Options) < optionsMade; distance++ {
			for _, j := range []int{i - distance, i + distance} {
				if j < 0 || j >= len(read) || len(q.Options) == optionsMade {
					continue
				}
				other := read[j]
				if (kindOf(other.answer) == kind) != sameKind || taken[fold(other.answer)] || other.text == read[i].text || overlaps(other.answer, read[i].answer) {
					continue
				}
				taken[fold(other.answer)] = true
				q.Options = append(q.Options, other.answer)
			}
		}
	}
	return q, len(q.Options) == optionsMade
}

// Kinds of answer, so that a figure's wrong options are figures too.
const (
	words = iota
	figure
	formula
)

// kindOf is an answer's kind: a figure, such as "537 500 so'm" or
// "22,16 %", a formula, such as "Yb+T = Yo+R", or words.
func kindOf(answer string) int {
	digits, letters := 0, 0
	for _, r := range answer {
		switch {
		case unicode.IsDigit(r):
			digits++
		case unicode.IsLetter(r):
			letters++
		}
	}
	switch {
	case digits > 0 && letters <= maxFigureLetters:
		return figure
	case strings.ContainsAny(answer, "=×/") && letters <= maxFigureLetters:
		return formula
	}
	return words
}

func overlaps(a, b string) bool {
	a, b = fold(a), fold(b)
	return strings.Contains(a, b) || strings.Contains(b, a)
}

func fold(text string) string {
	return strings.ToLower(text)
}
