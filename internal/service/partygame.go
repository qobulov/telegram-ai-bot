package service

// partyCommands are Truth or Dare's and Would You Rather's commands, as the
// owners asked on 2026-11-26, by the request each stands for: the model
// answers it with the party_game tool, to the member the command replies
// to, or else to the sender.
var partyCommands = map[string]string{
	"/haqiqat": "haqiqat ber",
	"/jurat":   "jur'at ber",
	"/tanla":   "nimani tanlarding, would you rather savoli ber",
	"/tod":     "truth or dare o'ynaymiz: guruhda yaqinda yozganlardan birini tasodifiy tanla va undan haqiqatmi yoki jur'atmi deb so'ra",
}
