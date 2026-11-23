package javdb

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
	"golang.org/x/text/language"

	"github.com/metatube-community/metatube-sdk-go/common/fetch"
	"github.com/metatube-community/metatube-sdk-go/common/parser"
	"github.com/metatube-community/metatube-sdk-go/model"
	"github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/metatube-community/metatube-sdk-go/provider/fc2/fc2util"
	"github.com/metatube-community/metatube-sdk-go/provider/internal/scraper"
)

var (
	_ provider.MovieProvider       = (*JavDB)(nil)
	_ provider.Fetcher             = (*JavDB)(nil)
	_ provider.ConfigSetter        = (*JavDB)(nil)
	_ provider.MovieURLHostAliases = (*JavDB)(nil)

	scorePattern = regexp.MustCompile(`\d+(?:\.\d+)?`)
	datePattern  = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
)

const (
	Name     = "JavDB"
	Priority = 1000 - 10
	baseURL  = "https://javdb.com/"
)

// JavDB reads basic FC2 metadata from search results, optionally using an
// existing JavDB session.
type JavDB struct {
	*fetch.Fetcher
	*scraper.Scraper
	cookie           string
	cookieFile       string
	cookieSavedAfter time.Time
	gate             requestGate
}

func New() *JavDB {
	return &JavDB{
		Fetcher: fetch.Default(&fetch.Config{Referer: baseURL}),
		Scraper: scraper.NewDefaultScraper(Name, baseURL, Priority, language.Japanese,
			scraper.WithHeaders(map[string]string{"Referer": baseURL})),
	}
}

func (jav *JavDB) SetConfig(config provider.Config) error {
	if config.Has("interval") {
		interval, err := config.GetDuration("interval")
		if err != nil || interval <= 0 {
			return errors.New("JavDB interval must be a positive duration")
		}
		jav.gate.interval = interval
	}
	if value, _ := config.GetString("pause_until"); value != "" {
		until, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return errors.New("JavDB pause_until must be an RFC3339 timestamp")
		}
		jav.gate.pauseUntil = until
	}
	value, _ := config.GetString("cookie")
	file, _ := config.GetString("cookie_file")
	value, file = strings.TrimSpace(value), strings.TrimSpace(file)
	if stamp, _ := config.GetString("cookie_saved_after"); stamp != "" {
		after, err := time.Parse(time.RFC3339, stamp)
		if err != nil || file == "" {
			return errors.New("JavDB cookie_saved_after requires an RFC3339 timestamp and cookie_file")
		}
		jav.cookieSavedAfter = after
	}
	if file != "" {
		if value != "" {
			return errors.New("set either cookie or cookie_file")
		}
		if _, _, err := readCookieFile(file); err != nil {
			return err
		}
		jav.cookieFile = file
	}
	if err := validateCookie(value); err != nil {
		return err
	}
	jav.cookie = value
	if jav.cookie == "" && jav.cookieFile == "" {
		return nil
	}
	c := jav.ClonedCollector()
	c.DisableCookies()
	c.SetRedirectHandler(func(req *http.Request, via []*http.Request) error {
		// The Cookie goes only on the initial HTTPS search, never a redirect.
		req.Header.Del("Cookie")
		start := via[0]
		if start.Header.Get("Cookie") != "" {
			switch strings.TrimSuffix(req.URL.Path, "/") {
			case "/login", "/users/login", "/users/sign_in":
				return errLoginRequired
			}
			if req.URL.Scheme != start.URL.Scheme || req.URL.Host != start.URL.Host {
				return errLoginRequired
			}
		}
		if len(via) >= 10 {
			return errors.New("too many JavDB redirects")
		}
		return nil
	})
	return nil
}

func readCookieFile(path string) (string, time.Time, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", time.Time{}, errors.New("cannot read JavDB cookie_file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", time.Time{}, errors.New("cannot stat JavDB cookie_file")
	}
	contents, err := io.ReadAll(file)
	if err != nil {
		return "", time.Time{}, errors.New("cannot read JavDB cookie_file")
	}
	value := strings.TrimSpace(string(contents))
	return value, info.ModTime(), validateCookie(value)
}

func validateCookie(value string) error {
	if value != "" {
		if _, err := http.ParseCookie(value); err != nil {
			return errors.New("invalid JavDB Cookie header value")
		}
	}
	return nil
}

func (jav *JavDB) visitSearch(c *colly.Collector, homepage string) error {
	cookie := jav.cookie
	stale := false
	if jav.cookieFile != "" {
		value, saved, err := readCookieFile(jav.cookieFile)
		if err != nil {
			return err
		}
		cookie = value
		stale = saved.Before(jav.cookieSavedAfter)
	}
	if cookie != "" && jav.URL().Scheme != "https" {
		return errors.New("JavDB Cookie requires HTTPS")
	}
	if err := jav.gate.begin(cookie, stale); err != nil {
		return err
	}
	var status int
	var retryAfter string
	var loginRequired bool
	defer func() { jav.gate.finish(cookie, loginRequired, status, retryAfter) }()
	c.OnError(func(r *colly.Response, _ error) {
		if r != nil {
			status = r.StatusCode
			if r.Headers != nil {
				retryAfter = r.Headers.Get("Retry-After")
			}
		}
	})
	if cookie == "" {
		return c.Visit(homepage)
	}
	c.OnHTML("form[action^='/login'] input[type='password'], form[action^='/users/sign_in'] input[type='password']", func(_ *colly.HTMLElement) {
		loginRequired = true
	})
	headers := http.Header{"Cookie": {cookie}, "Referer": {baseURL}}
	err := c.Request(http.MethodGet, homepage, nil, nil, headers)
	if errors.Is(err, errLoginRequired) {
		loginRequired = true
	}
	if loginRequired {
		return errLoginRequired
	}
	return err
}

func (jav *JavDB) NormalizeMovieID(id string) string {
	return fc2util.ParseNumber(strings.TrimSpace(id))
}

func (*JavDB) MovieURLHostAliases() []string {
	return []string{"www.javdb.com"}
}

func (jav *JavDB) ParseMovieIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Hostname() != "javdb.com" && u.Hostname() != "www.javdb.com") ||
		u.Path != "/search" {
		return "", provider.ErrInvalidURL
	}
	if id := jav.NormalizeMovieID(u.Query().Get("q")); id != "" {
		return id, nil
	}
	return "", provider.ErrInvalidURL
}

func (jav *JavDB) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
	id, err := jav.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	return jav.GetMovieInfoByID(id)
}

func (jav *JavDB) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	id = jav.NormalizeMovieID(id)
	if id == "" {
		return nil, provider.ErrInvalidID
	}

	q := url.Values{"q": {"FC2-" + id}, "f": {"all"}}
	homepage := jav.URL().ResolveReference(&url.URL{Path: "/search", RawQuery: q.Encode()}).String()
	var info *model.MovieInfo
	c := jav.ClonedCollector()
	c.OnHTML(".movie-list .item", func(e *colly.HTMLElement) {
		// Search pages also contain related items. Never use the first result
		// without checking its number, including its FC2 prefix.
		number := strings.TrimSpace(e.ChildText(".video-title strong"))
		if !strings.HasPrefix(strings.ToUpper(number), "FC2") ||
			jav.NormalizeMovieID(number) != id || info != nil {
			return
		}
		title := strings.TrimSpace(e.ChildAttr("a.box", "title"))
		if title == "" {
			title = strings.TrimSpace(strings.TrimPrefix(
				strings.TrimSpace(e.ChildText(".video-title")), number))
		}
		cover := e.ChildAttr(".cover img", "src")
		if cover == "" {
			cover = e.ChildAttr(".cover img", "data-src")
		}
		if cover != "" {
			cover = e.Request.AbsoluteURL(cover)
		}
		info = &model.MovieInfo{
			ID:            id,
			Number:        "FC2-" + id,
			Title:         title,
			Provider:      jav.Name(),
			Homepage:      homepage,
			ThumbURL:      cover,
			CoverURL:      cover,
			Score:         parser.ParseScore(scorePattern.FindString(e.ChildText(".score .value"))),
			ReleaseDate:   parser.ParseDate(datePattern.FindString(e.ChildText(".meta"))),
			Actors:        []string{},
			Genres:        []string{},
			PreviewImages: []string{},
		}
	})
	if err := jav.visitSearch(c, homepage); err != nil {
		return nil, err
	}
	if info == nil {
		return nil, provider.ErrInfoNotFound
	}
	if !info.IsValid() {
		return nil, provider.ErrIncompleteMetadata
	}
	return info, nil
}

func init() {
	provider.Register(Name, New)
}
