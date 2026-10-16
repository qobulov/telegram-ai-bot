package telegram

import "slices"

// ReactionEmoji are the only emoji Telegram accepts as a reaction from a
// bot, written exactly as the Bot API lists them (without the U+FE0F
// variation selector). Telegram refuses any other emoji, so a reaction the
// model picks is checked against this list before it is sent.
var ReactionEmoji = []string{
	"❤", "👍", "👎", "🔥", "🥰", "👏", "😁", "🤔", "🤯", "😱",
	"🤬", "😢", "🎉", "🤩", "🤮", "💩", "🙏", "👌", "🕊", "🤡",
	"🥱", "🥴", "😍", "🐳", "❤‍🔥", "🌚", "🌭", "💯", "🤣", "⚡",
	"🍌", "🏆", "💔", "🤨", "😐", "🍓", "🍾", "💋", "🖕", "😈",
	"😴", "😭", "🤓", "👻", "👨‍💻", "👀", "🎃", "🙈", "😇", "😨",
	"🤝", "✍", "🤗", "🫡", "🎅", "🎄", "☃", "💅", "🤪", "🗿",
	"🆒", "💘", "🙉", "🦄", "😘", "💊", "🙊", "😎", "👾", "🤷‍♂",
	"🤷", "🤷‍♀", "😡",
}

// IsReactionEmoji reports whether Telegram accepts emoji as a reaction.
func IsReactionEmoji(emoji string) bool {
	return slices.Contains(ReactionEmoji, emoji)
}
