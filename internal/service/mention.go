package service

import (
	"context"
	"regexp"
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

// maxMemberTags is how many members an answer may tag for someone who is
// not an admin, as the owners asked on 2026-11-24: calling everyone is for
// the group's admins, the bot's admins and its owners.
const maxMemberTags = 3

var (
	usernameTag = regexp.MustCompile(`(^|[^\w@])@([A-Za-z][A-Za-z0-9_]{3,31})\b`)
	idTag       = regexp.MustCompile(`\[([^\]\n]*)\]\(tg://user\?id=\d+\)`)
)

// memberTags counts the members text tags, by @username or by ID.
func memberTags(text string) int {
	seen := map[string]bool{}
	for _, m := range usernameTag.FindAllStringSubmatch(text, -1) {
		seen[strings.ToLower(m[2])] = true
	}
	for _, m := range idTag.FindAllString(text, -1) {
		seen[m] = true
	}
	return len(seen)
}

// untagged is text with its tags written as plain names, which tell no one.
func untagged(text string) string {
	text = idTag.ReplaceAllString(text, "$1")
	return usernameTag.ReplaceAllString(text, "$1$2")
}

// withoutMassTags is the answer for the group, with its tags written as
// plain names when it tags more than maxMemberTags members for someone who
// is not an admin.
func (s *Service) withoutMassTags(ctx context.Context, in incoming, text string) string {
	if !in.group || memberTags(text) <= maxMemberTags || s.opts.IsAdmin(in.userID) || s.isOwner(in.msg.From) || s.isAdminAsking(ctx, in) {
		return text
	}
	s.opts.Logger.Info("an answer's tags were removed: tagging many is for admins", "user_id", in.userID, "chat_id", in.scope.ChatID)
	return untagged(text)
}
