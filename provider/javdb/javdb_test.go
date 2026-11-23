package javdb

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"

	"github.com/metatube-community/metatube-sdk-go/common/fetch"
	mterrors "github.com/metatube-community/metatube-sdk-go/errors"
	"github.com/metatube-community/metatube-sdk-go/internal/envconfig"
	"github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/metatube-community/metatube-sdk-go/provider/internal/scraper"
	"github.com/metatube-community/metatube-sdk-go/provider/internal/testkit"
)

func newTestProvider(t *testing.T, handler http.HandlerFunc) *JavDB {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jav := New()
	jav.Scraper = scraper.NewDefaultScraper(Name, server.URL, Priority, language.Japanese)
	jav.gate.interval = time.Nanosecond
	return jav
}

func card(id, title, cover string) string {
	return fmt.Sprintf(`<div class="item"><a class="box" href="/v/example" title="%s">
<div class="cover"><img src="%s"></div>
<div class="video-title"><strong>%s</strong> %s</div>
<div class="score"><span class="value">4.54分, 由100人評價</span></div>
<div class="meta">2022-10-30</div></a></div>`, title, cover, id, title)
}

func TestJavDB_FC2SearchMetadata(t *testing.T) {
	jav := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Cookie"))
		assert.Equal(t, "/search", r.URL.Path)
		assert.Equal(t, "FC2-3119569", r.URL.Query().Get("q"))
		assert.Equal(t, "all", r.URL.Query().Get("f"))
		fmt.Fprint(w, `<div class="movie-list">`+
			card("FC2-31195690", "Related item", "/wrong.jpg")+
			card("ABC-3119569", "Other publisher", "/wrong.jpg")+
			card("FC2-3119569", "Example title", "/cover.jpg")+`</div>`)
	})
	info, err := jav.GetMovieInfoByID("FC2-PPV-3119569")
	require.NoError(t, err)
	require.True(t, info.IsValid())
	assert.Equal(t, "3119569", info.ID)
	assert.Equal(t, "FC2-3119569", info.Number)
	assert.Equal(t, "Example title", info.Title)
	assert.Equal(t, Name, info.Provider)
	assert.Equal(t, jav.URL().String()+"/cover.jpg", info.CoverURL)
	assert.Equal(t, info.CoverURL, info.ThumbURL)
	assert.InDelta(t, 4.54, info.Score, 0.001)
	assert.Equal(t, "2022-10-30", time.Time(info.ReleaseDate).Format("2006-01-02"))
	assert.Empty(t, info.Actors)
	// Persisted IDs can be looked up again without a login-only detail page.
	refetched, err := jav.GetMovieInfoByID(info.ID)
	require.NoError(t, err)
	assert.Equal(t, info, refetched)
	byURL, err := jav.GetMovieInfoByURL("https://javdb.com/search?q=FC2-PPV-3119569")
	require.NoError(t, err)
	assert.Equal(t, info, byURL)
}

func TestJavDB_ConfiguredCookie(t *testing.T) {
	for _, mode := range []string{"cookie", "cookie_file"} {
		t.Run(mode, func(t *testing.T) {
			const session = "_session=sample; preference=1"
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/search":
					if r.Header.Get("Cookie") != session {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					_, _ = fmt.Fprint(w, "<div class='movie-list'>"+card("FC2-3119569", "Example", "/cover.jpg")+"</div>")
				case "/cover.jpg":
					assert.Empty(t, r.Header.Get("Cookie"))
					_, _ = fmt.Fprint(w, "test image")
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			jav := &JavDB{
				Fetcher: fetch.Default(&fetch.Config{Transport: server.Client().Transport}),
				Scraper: scraper.NewDefaultScraper(Name, server.URL, Priority, language.Japanese,
					scraper.WithTransport(server.Client().Transport)),
			}
			cfg := envconfig.NewConfig()
			switch mode {
			case "cookie":
				cfg.Set(mode, session)
			case "cookie_file":
				file := filepath.Join(t.TempDir(), "javdb_cookie")
				require.NoError(t, os.WriteFile(file, []byte(session+"\n"), 0600))
				cfg.Set(mode, file)
			}
			require.NoError(t, jav.SetConfig(cfg))
			info, err := jav.GetMovieInfoByID("3119569")
			require.NoError(t, err)
			res, err := jav.Fetch(info.CoverURL)
			require.NoError(t, err)
			require.NoError(t, res.Body.Close())
		})
	}
}

func TestJavDB_InvalidCookieIsRedacted(t *testing.T) {
	cfg := envconfig.NewConfig()
	cfg.Set("cookie", "session=example\r\nbad")
	err := New().SetConfig(cfg)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "example")
}

func TestJavDB_CookieFileReloadsWithoutForwardingCookies(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			if r.Header.Get("Cookie") == "session=redirect" {
				http.Redirect(w, r, "/result", http.StatusFound)
				return
			}
			_, _ = fmt.Fprint(w, "<div class='movie-list'>"+card("FC2-3119569", "Example "+r.Header.Get("Cookie"), "/cover.jpg")+"</div>")
		case "/result":
			assert.Empty(t, r.Header.Get("Cookie"))
			_, _ = fmt.Fprint(w, "<div class='movie-list'>"+card("FC2-3119569", "Redirect", "/cover.jpg")+"</div>")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	jav := &JavDB{Scraper: scraper.NewDefaultScraper(Name, server.URL, Priority, language.Japanese,
		scraper.WithTransport(server.Client().Transport))}
	jav.gate.interval = time.Nanosecond
	file := filepath.Join(t.TempDir(), "javdb_cookie")
	require.NoError(t, os.WriteFile(file, nil, 0600))
	cfg := envconfig.NewConfig()
	cfg.Set("cookie_file", file)
	require.NoError(t, jav.SetConfig(cfg))

	for _, row := range []struct{ cookie, title string }{
		{"", "Example"},
		{"session=first", "Example session=first"},
		{"session=redirect", "Redirect"},
		{"session=second", "Example session=second"},
		{"", "Example"},
	} {
		next := file + ".new"
		require.NoError(t, os.WriteFile(next, []byte(row.cookie), 0600))
		require.NoError(t, os.Rename(next, file))
		info, err := jav.GetMovieInfoByID("3119569")
		require.NoError(t, err)
		assert.Equal(t, row.title, info.Title)
	}
}

func TestJavDB_FetchImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, baseURL, r.Referer())
		assert.NotEmpty(t, r.UserAgent())
		w.Header().Set("Content-Type", "image/jpeg")
		fmt.Fprint(w, "image bytes")
	}))
	t.Cleanup(server.Close)
	resp, err := New().Fetch(server.URL + "/cover.jpg")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "image/jpeg", resp.Header.Get("Content-Type"))
	assert.Equal(t, "image bytes", string(body))
}

func TestJavDB_MissingOrIncompleteMetadata(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		err  error
	}{
		{"empty", `<div class="movie-list"></div>`, provider.ErrInfoNotFound},
		{"related only", `<div class="movie-list">` + card("FC2-31195690", "Related", "/cover.jpg") + `</div>`, provider.ErrInfoNotFound},
		{"login page", `<form action="/login">Login</form>`, provider.ErrInfoNotFound},
		{"missing cover", `<div class="movie-list">` + card("FC2-3119569", "Example", "") + `</div>`, provider.ErrIncompleteMetadata},
		{"missing title", `<div class="movie-list">` + card("FC2-3119569", "", "/cover.jpg") + `</div>`, provider.ErrIncompleteMetadata},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jav := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, tc.body)
			})
			info, err := jav.GetMovieInfoByID("3119569")
			assert.Nil(t, info)
			assert.ErrorIs(t, err, tc.err)
		})
	}
}

func TestJavDB_HTTPError(t *testing.T) {
	jav := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	info, err := jav.GetMovieInfoByID("3119569")
	assert.Nil(t, info)
	require.Error(t, err)
	assert.False(t, errors.Is(err, provider.ErrInfoNotFound))
}

func TestJavDB_NormalizeMovieIDAndParseURL(t *testing.T) {
	jav := New()
	for _, id := range []string{"3119569", "FC2-3119569", "fc2-ppv-3119569", "FC2PPV3119569"} {
		assert.Equal(t, "3119569", jav.NormalizeMovieID(id))
	}
	for _, id := range []string{"SONE-065", "example", "FC2-31195690x", ""} {
		assert.Empty(t, jav.NormalizeMovieID(id))
		_, err := jav.GetMovieInfoByID(id)
		assert.ErrorIs(t, err, provider.ErrInvalidID)
	}
	for _, rawURL := range []string{
		"https://javdb.com/search?q=FC2-3119569&f=all",
		"https://www.javdb.com/search?q=FC2-PPV-3119569",
	} {
		id, err := jav.ParseMovieIDFromURL(rawURL)
		require.NoError(t, err)
		assert.Equal(t, "3119569", id)
	}
	for _, rawURL := range []string{
		"https://javdb.com/search?q=SONE-065",
		"https://javdb.com/v/example",
		"https://example.com/search?q=FC2-3119569",
		"https://javdb.com.example.com/search?q=FC2-3119569",
		"file:///search?q=FC2-3119569",
		"%",
	} {
		_, err := jav.GetMovieInfoByURL(rawURL)
		assert.ErrorIs(t, err, provider.ErrInvalidURL)
	}
}

func TestJavDB_TitleAndLazyImageFallback(t *testing.T) {
	jav := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<div class="movie-list"><div class="item"><a class="box">
<div class="cover"><img data-src="/lazy.jpg"></div>
<div class="video-title"><strong>FC2-3119569</strong> Example &amp; title</div>
</a></div></div>`)
	})
	info, err := jav.GetMovieInfoByID("3119569")
	require.NoError(t, err)
	assert.Equal(t, "Example & title", info.Title)
	assert.Contains(t, info.CoverURL, "/lazy.jpg")
}

func TestJavDB_ConcurrentLookups(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	jav := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		fmt.Fprint(w, `<div class="movie-list">`+card(r.URL.Query().Get("q"), "Example", "/cover.jpg")+`</div>`)
	})
	jav.gate.interval = time.Minute
	done := make(chan error, 1)
	go func() {
		_, err := jav.GetMovieInfoByID("3119569")
		done <- err
	}()
	<-started
	_, concurrent := jav.GetMovieInfoByURL("https://javdb.com/search?q=FC2-3108774")
	close(release)
	require.NoError(t, <-done)
	assertHTTPStatus(t, concurrent, http.StatusTooManyRequests)
	_, err := jav.GetMovieInfoByID("3108774")
	assertHTTPStatus(t, err, http.StatusTooManyRequests)
}

func assertHTTPStatus(t *testing.T, err error, status int) {
	t.Helper()
	var httpError *mterrors.HTTPError
	require.ErrorAs(t, err, &httpError)
	assert.Equal(t, status, httpError.Code)
}

func TestJavDB_RetryAfter(t *testing.T) {
	var calls atomic.Int32
	jav := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "7200")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := jav.GetMovieInfoByID("3119569")
	require.Error(t, err)
	assert.Greater(t, time.Until(jav.gate.next), time.Hour)
	_, err = jav.GetMovieInfoByID("3108774")
	assertHTTPStatus(t, err, http.StatusTooManyRequests)
	assert.EqualValues(t, 1, calls.Load())
}

func TestJavDB_PauseAndRejectInvalidCookie(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/search", r.URL.Path)
		if r.Header.Get("Cookie") == "session=invalid" {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		_, _ = fmt.Fprint(w, `<div class="movie-list">`+card("FC2-3119569", "Example", "/cover.jpg")+`</div>`)
	}))
	t.Cleanup(server.Close)
	jav := &JavDB{Scraper: scraper.NewDefaultScraper(Name, server.URL, Priority, language.Japanese,
		scraper.WithTransport(server.Client().Transport))}
	file := filepath.Join(t.TempDir(), "javdb_cookie")
	require.NoError(t, os.WriteFile(file, []byte("session=invalid"), 0600))
	now := time.Now()
	old := now.Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(file, old, old))
	cfg := envconfig.NewConfig()
	cfg.Set("cookie_file", file)
	cfg.Set("interval", "1ns")
	cfg.Set("pause_until", now.Add(time.Hour).Format(time.RFC3339))
	cfg.Set("cookie_saved_after", now.Add(-time.Hour).Format(time.RFC3339))
	require.NoError(t, jav.SetConfig(cfg))
	_, err := jav.GetMovieInfoByID("3119569")
	assertHTTPStatus(t, err, http.StatusServiceUnavailable)
	jav.gate.pauseUntil = now.Add(-time.Hour)
	_, err = jav.GetMovieInfoByID("3119569")
	assertHTTPStatus(t, err, http.StatusUnauthorized)
	assert.True(t, jav.gate.next.IsZero())

	require.NoError(t, os.Chtimes(file, now, now))
	_, err = jav.GetMovieInfoByID("3119569")
	assertHTTPStatus(t, err, http.StatusUnauthorized)
	next := jav.gate.next
	assert.False(t, next.IsZero())
	_, err = jav.GetMovieInfoByID("3119569")
	assertHTTPStatus(t, err, http.StatusUnauthorized)
	assert.Equal(t, next, jav.gate.next)

	require.NoError(t, os.WriteFile(file, []byte("session=fresh"), 0600))
	_, err = jav.GetMovieInfoByID("3119569")
	require.NoError(t, err)
}

func TestJavDB_GetMovieInfoByID(t *testing.T) {
	testkit.Test(t, New, []string{"FC2-3119569"})
}
