package service

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// labelMarks are what would break the tag's [label](link).
var labelMarks = strings.NewReplacer("[", "", "]", "", "(", "", ")", "", "\n", " ", "*", "", "`", "")

// mentionMember tags a member by ID, which reaches them with or without a
// @username: their first name, without the @username the log adds.
func mentionMember(userID int64, name string) string {
	if at := strings.Index(name, " @"); at >= 0 {
		name = name[:at]
	}
	name = strings.TrimSpace(labelMarks.Replace(name))
	if utf8.RuneCountInString(name) > 32 {
		name = string([]rune(name)[:32])
	}
	if name == "" {
		name = "👤"
	}
	return "[" + name + "](tg://user?id=" + strconv.FormatInt(userID, 10) + ")"
}
