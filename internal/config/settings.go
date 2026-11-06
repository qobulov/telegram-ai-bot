package config

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Setting is an environment variable an owner may change from Telegram with
// /set: for the whole bot, and when Group, for one group too. The value
// saved goes over the environment's.
type Setting struct {
	Name string
	// Group settings may be set for one group as well as for the bot.
	Group bool
	// Max bounds a number, where Load sets only a minimum; 0 is no bound.
	Max   int
	About string
}

// Settings are the environment variables /set may change.
var Settings = []Setting{
	{Name: "GROUP_RATE_LIMIT_PER_MINUTE", Group: true, Max: 60, About: "guruhda daqiqasiga javoblar"},
	{Name: "GROUP_MIN_ACTIVE_MEMBERS", Group: true, Max: 1000, About: "javob uchun kamida nechta faol a'zo (0 o'chiq)"},
	{Name: "GROUP_ACTIVE_DAYS", Group: true, Max: 30, About: "faol a'zolar necha kunda sanaladi"},
	{Name: "MAX_GROUP_REPLY_LENGTH", Group: true, Max: 4096, About: "guruhdagi javobning eng ko'p belgisi"},
	{Name: "GROUP_MIN_MEMBERS", Max: 100000, About: "boshqa guruhlarda ishlash uchun kamida a'zolar"},
	{Name: "PRIVATE_CHATS", About: "shaxsiy chat: on, owners yoki off"},
	{Name: "PRIVATE_DAILY_LIMIT", Max: 10000, About: "shaxsiy chatda kuniga javoblar (0 cheksiz)"},
	{Name: "PREMIUM_PRICE_STARS", Max: 10000, About: "premium narxi, Telegram Stars'da (0 sotilmaydi)"},
	{Name: "PREMIUM_DAYS", Max: 365, About: "premium necha kun amal qiladi"},
	{Name: "PREMIUM_DAILY_LIMIT", Max: 10000, About: "premiumda shaxsiy chatda kuniga javoblar"},
	{Name: "BUSINESS_DAILY_LIMIT", Max: 10000, About: "biznes akkauntda har bir mijozga kuniga javoblar (0 cheksiz)"},
	{Name: "USER_RATE_LIMIT_PER_MINUTE", Max: 120, About: "bir odamga daqiqasiga javoblar"},
	{Name: "DAILY_IMAGE_LIMIT", Max: 100, About: "kuniga rasm chizish (0 o'chiq)"},
	{Name: "ENABLE_IMAGE_GENERATION", About: "rasm chizish: true yoki false"},
	{Name: "ENABLE_REPLY_TRIGGER", About: "botga reply qilinganda javob: true yoki false"},
	{Name: "ENABLE_WAKE_WORD_TRIGGER", About: "ismini aytganda javob: true yoki false"},
	{Name: "ENABLE_USERNAME_MENTION", About: "@username bilan chaqirilganda javob: true yoki false"},
	{Name: "RETENTION_DAYS", Max: 365, About: "suhbat tarixi necha kun saqlanadi (0 doim)"},
	{Name: "MAX_HISTORY_MESSAGES", Max: 100, About: "modelga beriladigan oldingi xabarlar"},
	{Name: "GROUP_AMBIENT_MESSAGES", Max: 2000, About: "guruh logidan o'qiladigan xabarlar (0 o'chiq)"},
	{Name: "MAX_OUTPUT_TOKENS", Max: 8192, About: "modelning eng uzun javobi, tokenda"},
}

// FindSetting is the setting called name, in any case.
func FindSetting(name string) (Setting, bool) {
	i := slices.IndexFunc(Settings, func(s Setting) bool { return strings.EqualFold(s.Name, name) })
	if i < 0 {
		return Setting{}, false
	}
	return Settings[i], true
}

// Overlay reads a variable from overrides first, then from getenv.
func Overlay(getenv func(string) string, overrides map[string]string) func(string) string {
	return func(name string) string {
		if value, ok := overrides[name]; ok {
			return value
		}
		return getenv(name)
	}
}

// CheckSetting reports why value cannot be the setting's, read as Load
// reads it with the rest of the environment from getenv, or nil.
func CheckSetting(getenv func(string) string, setting Setting, value string) error {
	if setting.Max > 0 {
		if n, err := strconv.Atoi(value); err == nil && n > setting.Max {
			return fmt.Errorf("%s must be at most %d", setting.Name, setting.Max)
		}
	}
	_, err := Load(Overlay(getenv, map[string]string{setting.Name: value}))
	return err
}
