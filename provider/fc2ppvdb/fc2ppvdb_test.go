package fc2ppvdb

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"

	"github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/metatube-community/metatube-sdk-go/provider/internal/scraper"

	"github.com/metatube-community/metatube-sdk-go/provider/internal/testkit"
)

func TestFC2PPVDB_MigratedArticle(t *testing.T) {
	const page = `{"component":"Articles/Show","props":{"article":{
		"id":123456,"video_id":4925979,"title":"Test title","image_url":"/cover.jpg",
		"release_date":"2026-01-02","duration":"00:12:34","writer":{"name":"Publisher"},
		"tags":[{"name":" Tag "},{"name":""}]}}}`
	for _, tc := range []struct {
		name string
		body string
	}{
		{"script", `<script type="application/json" data-page="app">` + page + `</script>`},
		{"attribute", `<div id="app" data-page="` + html.EscapeString(page) + `"></div>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/articles/4925979", r.URL.Path)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			t.Cleanup(server.Close)
			s := &FC2PPVDB{scraper.NewDefaultScraper(Name, server.URL, Priority, language.Japanese)}
			info, err := s.GetMovieInfoByURL("https://fc2ppvdb.com/articles/4925979/")
			require.NoError(t, err)
			assert.Equal(t, "4925979", info.ID)
			assert.Equal(t, "FC2-4925979", info.Number)
			assert.Equal(t, Name, info.Provider)
			assert.Equal(t, server.URL+"/articles/4925979", info.Homepage)
			assert.Equal(t, server.URL+"/cover.jpg", info.CoverURL)
			assert.Equal(t, info.CoverURL, info.ThumbURL)
			assert.Equal(t, "Test title", info.Title)
			assert.Equal(t, "Publisher", info.Maker)
			assert.ElementsMatch(t, []string{"Tag"}, []string(info.Genres))
			assert.Equal(t, "2026-01-02", time.Time(info.ReleaseDate).Format("2006-01-02"))
			assert.Positive(t, info.Runtime)
		})
	}
}

func TestFC2PPVDB_RejectsUnrelatedArticle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `<script type="application/json" data-page="app">{"component":"Articles/Show",`+
			`"props":{"article":{"video_id":4925980,"title":"Unrelated","image_url":"/cover.jpg"}}}</script>`)
	}))
	t.Cleanup(server.Close)
	s := &FC2PPVDB{scraper.NewDefaultScraper(Name, server.URL, Priority, language.Japanese)}
	_, err := s.GetMovieInfoByID("FC2-PPV-4925979")
	assert.ErrorIs(t, err, provider.ErrInfoNotFound)
	_, err = s.GetMovieInfoByURL("https://example.com/articles/4925979")
	assert.ErrorIs(t, err, provider.ErrInvalidURL)
}

func TestFC2PPVDB_GetMovieInfoByID(t *testing.T) {
	testkit.Test(t, New, []string{
		"2812904",
		"4669533",
		"4745474",
		"4137487",
	})
}
