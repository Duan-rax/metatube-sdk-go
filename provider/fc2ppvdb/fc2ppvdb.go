package fc2ppvdb

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/gocolly/colly/v2"
	"golang.org/x/text/language"

	"github.com/metatube-community/metatube-sdk-go/common/parser"
	"github.com/metatube-community/metatube-sdk-go/model"
	"github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/metatube-community/metatube-sdk-go/provider/fc2/fc2util"
	"github.com/metatube-community/metatube-sdk-go/provider/internal/scraper"
)

var (
	_ provider.MovieProvider       = (*FC2PPVDB)(nil)
	_ provider.MovieURLHostAliases = (*FC2PPVDB)(nil)
)

const (
	Name     = "FC2PPVDB"
	Priority = 1000 - 2
)

const (
	baseURL     = "https://fc2cmadb.com/"
	articlePath = "/articles/"
)

type FC2PPVDB struct {
	*scraper.Scraper
}

type articlePage struct {
	Component string
	Props     struct {
		Article struct {
			VideoID     json.Number `json:"video_id"`
			Title       string
			ReleaseDate string `json:"release_date"`
			Duration    string
			ImageURL    string `json:"image_url"`
			Writer      struct{ Name string }
			Tags        []struct{ Name string }
		}
	}
}

func New() *FC2PPVDB {
	return &FC2PPVDB{scraper.NewDefaultScraper(Name, baseURL, Priority, language.Japanese)}
}

func (fc2ppvdb *FC2PPVDB) NormalizeMovieID(id string) string {
	return fc2util.ParseNumber(id)
}

func (*FC2PPVDB) MovieURLHostAliases() []string {
	return []string{"www.fc2cmadb.com", "fc2ppvdb.com", "www.fc2ppvdb.com"}
}

func (fc2ppvdb *FC2PPVDB) ParseMovieIDFromURL(rawURL string) (string, error) {
	page, err := url.Parse(rawURL)
	if err != nil || (page.Scheme != "https" && page.Scheme != "http") || page.User != nil {
		return "", provider.ErrInvalidURL
	}
	switch strings.ToLower(page.Hostname()) {
	case "fc2cmadb.com", "www.fc2cmadb.com", "fc2ppvdb.com", "www.fc2ppvdb.com":
	default:
		return "", provider.ErrInvalidURL
	}
	number, found := strings.CutPrefix(strings.TrimSuffix(page.Path, "/"), articlePath)
	if !found || number == "" || fc2ppvdb.NormalizeMovieID(number) != number {
		return "", provider.ErrInvalidURL
	}
	return number, nil
}

func (fc2ppvdb *FC2PPVDB) GetMovieInfoByURL(rawURL string) (*model.MovieInfo, error) {
	id, err := fc2ppvdb.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	return fc2ppvdb.GetMovieInfoByID(id)
}

func (fc2ppvdb *FC2PPVDB) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	id = fc2ppvdb.NormalizeMovieID(id)
	if id == "" {
		return nil, provider.ErrInvalidID
	}

	homepage := fc2ppvdb.URL().ResolveReference(&url.URL{Path: articlePath + id}).String()
	var info *model.MovieInfo
	c := fc2ppvdb.ClonedCollector()
	// The site's regular HTML includes the article as Inertia page data.
	c.OnHTML("script[type='application/json'][data-page], #app[data-page]", func(e *colly.HTMLElement) {
		if info != nil {
			return
		}
		payload := e.Attr("data-page")
		if e.Name == "script" {
			payload = e.Text
		}
		var page articlePage
		if err := json.Unmarshal([]byte(payload), &page); err != nil || page.Component != "Articles/Show" {
			return
		}
		article := page.Props.Article
		if article.VideoID.String() != id {
			return
		}
		cover := ""
		if image, err := url.Parse(strings.TrimSpace(article.ImageURL)); err == nil && image.String() != "" {
			resolved := e.Request.URL.ResolveReference(image)
			if (resolved.Scheme == "https" || resolved.Scheme == "http") && resolved.Host != "" && resolved.User == nil {
				cover = resolved.String()
			}
		}
		info = &model.MovieInfo{
			ID:            id,
			Number:        "FC2-" + id,
			Provider:      fc2ppvdb.Name(),
			Homepage:      homepage,
			Title:         strings.TrimSpace(article.Title),
			CoverURL:      cover,
			ThumbURL:      cover,
			Maker:         strings.TrimSpace(article.Writer.Name),
			ReleaseDate:   parser.ParseDate(article.ReleaseDate),
			Runtime:       parser.ParseRuntime(article.Duration),
			Actors:        []string{},
			PreviewImages: []string{},
			Genres:        make([]string, 0, len(article.Tags)),
		}
		for _, tag := range article.Tags {
			if name := strings.TrimSpace(tag.Name); name != "" {
				info.Genres = append(info.Genres, name)
			}
		}
	})

	if err := c.Visit(homepage); err != nil {
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
