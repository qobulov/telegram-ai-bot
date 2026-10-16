package telegram

import "slices"

// ReactionEmoji are the only emoji Telegram accepts as a reaction from a
// bot, written exactly as the Bot API lists them (without the U+FE0F
// variation selector). Telegram refuses any other emoji, so a reaction the
// model picks is checked against this list before it is sent. Four entries
// are sequences joined by the invisible U+200D, written \u200d here so the
// joiner can be seen.
var ReactionEmoji = []string{
	"❤", "👍", "👎", "🔥", "🥰", "👏", "😁", "🤔", "🤯", "😱",
	"🤬", "😢", "🎉", "🤩", "🤮", "💩", "🙏", "👌", "🕊", "🤡",
	"🥱", "🥴", "😍", "🐳", "❤\u200d🔥", "🌚", "🌭", "💯", "🤣", "⚡",
	"🍌", "🏆", "💔", "🤨", "😐", "🍓", "🍾", "💋", "🖕", "😈",
	"😴", "😭", "🤓", "👻", "👨\u200d💻", "👀", "🎃", "🙈", "😇", "😨",
	"🤝", "✍", "🤗", "🫡", "🎅", "🎄", "☃", "💅", "🤪", "🗿",
	"🆒", "💘", "🙉", "🦄", "😘", "💊", "🙊", "😎", "👾", "🤷\u200d♂",
	"🤷", "🤷\u200d♀", "😡",
}

// IsReactionEmoji reports whether emoji is one of the reactions Telegram
// allows bots to use. A group may allow fewer, so Telegram can still refuse
// an emoji this accepts.
func IsReactionEmoji(emoji string) bool {
	return slices.Contains(ReactionEmoji, emoji)
}
