package javdb

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

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
	cookieFile string
}

func New() *JavDB {
	return &JavDB{
		Fetcher: fetch.Default(&fetch.Config{Referer: baseURL}),
		Scraper: scraper.NewDefaultScraper(Name, baseURL, Priority, language.Japanese,
			scraper.WithHeaders(map[string]string{"Referer": baseURL})),
	}
}

func (jav *JavDB) SetConfig(config provider.Config) error {
	value, _ := config.GetString("cookie")
	file, _ := config.GetString("cookie_file")
	value, file = strings.TrimSpace(value), strings.TrimSpace(file)
	if file != "" {
		if value != "" {
			return errors.New("set either cookie or cookie_file")
		}
		if _, err := readCookieFile(file); err != nil {
			return err
		}
		jav.cookieFile = file
		c := jav.ClonedCollector()
		c.DisableCookies()
		c.SetRedirectHandler(func(req *http.Request, via []*http.Request) error {
			// The file supplies credentials only to the initial search request.
			req.Header.Del("Cookie")
			if len(via) >= 10 {
				return errors.New("too many JavDB redirects")
			}
			return nil
		})
		return nil
	}
	if value == "" {
		return nil
	}
	cookies, err := http.ParseCookie(value)
	if err != nil {
		return errors.New("invalid JavDB Cookie header value")
	}
	for _, cookie := range cookies {
		// Set host-only HTTPS cookies for search, independent of the image fetcher.
		cookie.Secure = true
		cookie.Path = "/search"
	}
	return jav.ClonedCollector().SetCookies(jav.URL().String(), cookies)
}

func readCookieFile(file string) (string, error) {
	contents, err := os.ReadFile(file)
	if err != nil {
		return "", errors.New("cannot read JavDB cookie_file")
	}
	value := strings.TrimSpace(string(contents))
	if value != "" {
		if _, err := http.ParseCookie(value); err != nil {
			return "", errors.New("invalid JavDB Cookie header value")
		}
	}
	return value, nil
}

func (jav *JavDB) visitSearch(c *colly.Collector, homepage string) error {
	if jav.cookieFile == "" {
		return c.Visit(homepage)
	}
	cookie, err := readCookieFile(jav.cookieFile)
	if err != nil {
		return err
	}
	if cookie == "" {
		return c.Visit(homepage)
	}
	if jav.URL().Scheme != "https" {
		return errors.New("JavDB Cookie requires HTTPS")
	}
	headers := http.Header{"Cookie": {cookie}, "Referer": {baseURL}}
	return c.Request(http.MethodGet, homepage, nil, nil, headers)
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
