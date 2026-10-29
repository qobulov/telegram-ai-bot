// Package guards asks outside services whether a member, a link or a
// picture is to be kept out of a group: spam lists of Telegram accounts
// (CAS, lols.bot), malicious links (Google Safe Browsing, URLhaus) and
// nudity in pictures (Sightengine). They answer only; the service acts.
package guards

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	requestTimeout = 8 * time.Second
	bodyLimit      = 256 << 10
)

// Options says where each service is and the keys of those that need one.
// Empty base URLs are the production ones.
type Options struct {
	HTTPClient        *http.Client
	CAS, Lols         string
	URLhaus           string
	URLhausKey        string
	SafeBrowsing      string
	SafeBrowsingKey   string
	Sightengine       string
	SightengineUser   string
	SightengineSecret string
}

// Checker asks the services.
type Checker struct {
	http *http.Client
	opts Options
}

// New returns a Checker; a nil HTTPClient means one with a short timeout.
func New(opts Options) *Checker {
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: requestTimeout}
	}
	if opts.CAS == "" {
		opts.CAS = "https://api.cas.chat"
	}
	if opts.Lols == "" {
		opts.Lols = "https://api.lols.bot"
	}
	if opts.URLhaus == "" {
		opts.URLhaus = "https://urlhaus-api.abuse.ch"
	}
	if opts.SafeBrowsing == "" {
		opts.SafeBrowsing = "https://safebrowsing.googleapis.com"
	}
	if opts.Sightengine == "" {
		opts.Sightengine = "https://api.sightengine.com"
	}
	return &Checker{http: opts.HTTPClient, opts: opts}
}

// CanCheckLinks and CanCheckImages report whether the keys those need are
// set.
func (c *Checker) CanCheckLinks() bool {
	return c.opts.URLhausKey != "" || c.opts.SafeBrowsingKey != ""
}
func (c *Checker) CanCheckImages() bool {
	return c.opts.SightengineUser != "" && c.opts.SightengineSecret != ""
}

// Spammer reports whether a Telegram account is on a spam list, and which.
// One list failing does not hide the other's answer.
func (c *Checker) Spammer(ctx context.Context, userID int64) (string, bool, error) {
	var cas struct {
		OK bool `json:"ok"`
	}
	casErr := c.getJSON(ctx, c.opts.CAS+"/check?user_id="+strconv.FormatInt(userID, 10), nil, &cas)
	if casErr == nil && cas.OK {
		return "CAS", true, nil
	}
	var lols struct {
		OK     bool `json:"ok"`
		Banned bool `json:"banned"`
	}
	lolsErr := c.getJSON(ctx, c.opts.Lols+"/account?id="+strconv.FormatInt(userID, 10), nil, &lols)
	if lolsErr == nil && lols.Banned {
		return "lols.bot", true, nil
	}
	if casErr != nil && lolsErr != nil {
		return "", false, fmt.Errorf("guards: spam lists: %w; %w", casErr, lolsErr)
	}
	return "", false, nil
}

// MaliciousLink reports whether a link is to malware, phishing or unwanted
// software, and why: Google Safe Browsing judges the link, URLhaus its
// host, each when its key is set. One service failing does not hide the
// other's answer.
func (c *Checker) MaliciousLink(ctx context.Context, link string) (string, bool, error) {
	u, err := url.Parse(link)
	if err != nil || u.Hostname() == "" {
		return "", false, fmt.Errorf("guards: not a link: %q", link)
	}
	var errs []error
	if c.opts.SafeBrowsingKey != "" {
		reason, bad, err := c.safeBrowsing(ctx, link)
		if bad {
			return reason, true, nil
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	if c.opts.URLhausKey != "" {
		reason, bad, err := c.urlhausHost(ctx, strings.ToLower(u.Hostname()))
		if bad {
			return reason, true, nil
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 && len(errs) == c.linkServices() {
		return "", false, errors.Join(errs...)
	}
	return "", false, nil
}

// linkServices is how many services judge links.
func (c *Checker) linkServices() int {
	n := 0
	for _, key := range []string{c.opts.SafeBrowsingKey, c.opts.URLhausKey} {
		if key != "" {
			n++
		}
	}
	return n
}

// safeBrowsing asks Google Safe Browsing about a link. The key goes in a
// header, so that it is in no URL an error or a log could show.
func (c *Checker) safeBrowsing(ctx context.Context, link string) (string, bool, error) {
	query, err := json.Marshal(map[string]any{
		"client": map[string]string{"clientId": "maria-bot", "clientVersion": "1.0"},
		"threatInfo": map[string]any{
			"threatTypes":      []string{"MALWARE", "SOCIAL_ENGINEERING", "UNWANTED_SOFTWARE", "POTENTIALLY_HARMFUL_APPLICATION"},
			"platformTypes":    []string{"ANY_PLATFORM"},
			"threatEntryTypes": []string{"URL"},
			"threatEntries":    []map[string]string{{"url": link}},
		},
	})
	if err != nil {
		return "", false, err
	}
	var answer struct {
		Matches []struct {
			ThreatType string `json:"threatType"`
		} `json:"matches"`
	}
	header := http.Header{"X-Goog-Api-Key": {c.opts.SafeBrowsingKey}, "Content-Type": {"application/json"}}
	if err := c.postJSON(ctx, c.opts.SafeBrowsing+"/v4/threatMatches:find", header, bytes.NewReader(query), &answer); err != nil {
		return "", false, err
	}
	if len(answer.Matches) == 0 {
		return "", false, nil
	}
	return "Google: " + strings.ToLower(answer.Matches[0].ThreatType), true, nil
}

// urlhausHost reports whether URLhaus lists a host, by its own entries or
// the blocklists it relays (Spamhaus DBL, SURBL), and why.
func (c *Checker) urlhausHost(ctx context.Context, host string) (string, bool, error) {
	var answer struct {
		QueryStatus string `json:"query_status"`
		Blacklists  struct {
			SpamhausDBL string `json:"spamhaus_dbl"`
			SURBL       string `json:"surbl"`
		} `json:"blacklists"`
		URLs []struct {
			URLStatus string `json:"url_status"`
			Threat    string `json:"threat"`
		} `json:"urls"`
	}
	form := url.Values{"host": {host}}
	header := http.Header{"Auth-Key": {c.opts.URLhausKey}, "Content-Type": {"application/x-www-form-urlencoded"}}
	if err := c.postJSON(ctx, c.opts.URLhaus+"/v1/host/", header, strings.NewReader(form.Encode()), &answer); err != nil {
		return "", false, err
	}
	if answer.QueryStatus != "ok" {
		return "", false, nil
	}
	for _, u := range answer.URLs {
		if u.URLStatus == "online" {
			return "URLhaus: " + u.Threat, true, nil
		}
	}
	for _, listed := range []string{answer.Blacklists.SpamhausDBL, answer.Blacklists.SURBL} {
		if listed != "" && listed != "not listed" {
			return "URLhaus: " + listed, true, nil
		}
	}
	return "", false, nil
}

// unsafeScore is the probability above which a picture is taken as nude.
const unsafeScore = 0.7

// UnsafeImage reports whether Sightengine sees nudity in a picture. Each
// model is an operation of the plan, and with gore checked too the guarded
// groups' two hundred pictures a day spent the free plan's month in days.
// The picture is uploaded: a Telegram file link would give away the bot's
// token.
func (c *Checker) UnsafeImage(ctx context.Context, image []byte) (string, bool, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("models", "nudity-2.1")
	_ = form.WriteField("api_user", c.opts.SightengineUser)
	_ = form.WriteField("api_secret", c.opts.SightengineSecret)
	part, err := form.CreateFormFile("media", "image.jpg")
	if err != nil {
		return "", false, err
	}
	if _, err := part.Write(image); err != nil {
		return "", false, err
	}
	if err := form.Close(); err != nil {
		return "", false, err
	}
	var answer struct {
		Status string `json:"status"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
		Nudity map[string]any `json:"nudity"`
	}
	if err := c.postJSON(ctx, c.opts.Sightengine+"/1.0/check.json", http.Header{"Content-Type": {form.FormDataContentType()}}, &body, &answer); err != nil {
		return "", false, err
	}
	if answer.Status != "success" {
		return "", false, fmt.Errorf("guards: Sightengine: %s", answer.Error.Message)
	}
	for _, class := range []string{"sexual_activity", "sexual_display", "erotica", "very_suggestive"} {
		if score, ok := answer.Nudity[class].(float64); ok && score >= unsafeScore {
			return "nudity", true, nil
		}
	}
	return "", false, nil
}

func (c *Checker) getJSON(ctx context.Context, link string, header http.Header, into any) error {
	return c.do(ctx, http.MethodGet, link, header, nil, into)
}

func (c *Checker) postJSON(ctx context.Context, link string, header http.Header, body io.Reader, into any) error {
	return c.do(ctx, http.MethodPost, link, header, body, into)
}

func (c *Checker) do(ctx context.Context, method, link string, header http.Header, body io.Reader, into any) error {
	req, err := http.NewRequestWithContext(ctx, method, link, body)
	if err != nil {
		return err
	}
	for name, values := range header {
		req.Header[name] = values
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("guards: %s: %w", req.URL.Host, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, bodyLimit))
	if err != nil {
		return fmt.Errorf("guards: %s: %w", req.URL.Host, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("guards: %s answered %d%s", req.URL.Host, resp.StatusCode, refusalReason(raw))
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("guards: %s: %w", req.URL.Host, err)
	}
	return nil
}

// refusalReason is the reason a service gave with a refusal, such as
// Sightengine's "monthly limit reached", which a bare 400 left unknown.
func refusalReason(raw []byte) string {
	var answer struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &answer) != nil || answer.Error.Message == "" {
		return ""
	}
	return fmt.Sprintf(": %s (%s)", answer.Error.Message, answer.Error.Type)
}
