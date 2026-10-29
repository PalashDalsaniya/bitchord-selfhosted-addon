// Package navidrome reads a Navidrome server through its Subsonic API. It
// sticks to calls every Subsonic server answers, plus Navidrome's empty
// search3 query that lists every song.
package navidrome

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rairulyle/bitchord-selfhosted-addon/internal/media"
)

const (
	clientName = "bitchord-selfhosted-addon"
	// apiVersion is the Subsonic API version the addon speaks; servers
	// answer older clients too, so this is what Navidrome itself reports.
	apiVersion = "1.16.1"
)

type Options struct {
	BaseURL string
	// Credential is an API key, or the u, t and s parameters of a sign-in
	// as a query string; see Credential.
	Credential    string
	PageSize      int
	HeaderTimeout time.Duration
	CallTimeout   time.Duration
}

type Client struct {
	http     *media.Client
	pageSize int
}

func New(o Options) *Client {
	if o.PageSize <= 0 {
		o.PageSize = 500
	}
	auth := authParams(o.Credential)
	return &Client{
		http: media.NewClient(media.ClientOptions{
			BaseURL:  o.BaseURL,
			Name:     "navidrome",
			TokenVar: "the Navidrome credentials",
			Authorize: func(r *http.Request) {
				query := r.URL.Query()
				for key, values := range auth {
					query[key] = values
				}
				r.URL.RawQuery = query.Encode()
			},
			HeaderTimeout: o.HeaderTimeout,
			CallTimeout:   o.CallTimeout,
			RedactQuery:   true,
		}),
		pageSize: o.PageSize,
	}
}

// authParams adds the client parameters every Subsonic call needs to the
// credential's own.
func authParams(credential string) url.Values {
	out := url.Values{"v": {apiVersion}, "c": {clientName}, "f": {"json"}}
	if username, token, salt, ok := ParseCredential(credential); ok {
		out.Set("u", username)
		out.Set("t", token)
		out.Set("s", salt)
	} else {
		out.Set("apiKey", credential)
	}
	return out
}

func (c *Client) Name() string { return "Navidrome" }

// response is the envelope of every Subsonic answer. A failed call still
// answers 200, with status "failed" and an error code.
type response struct {
	Status        string     `json:"status"`
	ServerVersion string     `json:"serverVersion"`
	Version       string     `json:"version"`
	Error         *callError `json:"error"`
}

type callError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Subsonic error codes the addon tells apart.
const (
	codeWrongCredentials = 40
	codeTokenUnsupported = 41
	codeAuthConflict     = 43
	codeInvalidAPIKey    = 44
	codeNotFound         = 70
)

func (e *callError) asError() error {
	switch e.Code {
	case codeWrongCredentials, codeInvalidAPIKey, codeAuthConflict:
		return fmt.Errorf("navidrome rejected the Navidrome credentials: %w", media.ErrUnauthorized)
	case codeTokenUnsupported:
		return errors.New("navidrome does not accept a password sign-in for this user, paste an API key instead")
	case codeNotFound:
		return media.ErrNotFound
	}
	return fmt.Errorf("navidrome: error %d: %s", e.Code, e.Message)
}

// call runs one Subsonic method and decodes its answer into into, which
// gets the inside of the subsonic-response envelope.
func (c *Client) call(ctx context.Context, method string, query url.Values, into any) error {
	var answer struct {
		Response json.RawMessage `json:"subsonic-response"`
	}
	path := "/rest/" + method
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	if err := c.http.GetJSON(ctx, path, nil, &answer); err != nil {
		return err
	}
	return decode(answer.Response, into)
}

func decode(raw json.RawMessage, into any) error {
	if len(raw) == 0 {
		return errors.New("navidrome: malformed answer: no subsonic-response")
	}
	var envelope response
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("navidrome: malformed answer: %w", err)
	}
	if envelope.Error != nil {
		return envelope.Error.asError()
	}
	if envelope.Status != "ok" {
		return fmt.Errorf("navidrome: answered status %q", envelope.Status)
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("navidrome: malformed answer: %w", err)
	}
	return nil
}

func (c *Client) Version(ctx context.Context) (string, error) {
	var envelope response
	if err := c.call(ctx, "ping", nil, &envelope); err != nil {
		return "", err
	}
	if envelope.ServerVersion != "" {
		return envelope.ServerVersion, nil
	}
	return envelope.Version, nil
}

func (c *Client) Libraries(ctx context.Context) ([]media.Library, error) {
	var answer struct {
		MusicFolders struct {
			MusicFolder []Folder `json:"musicFolder"`
		} `json:"musicFolders"`
	}
	if err := c.call(ctx, "getMusicFolders", nil, &answer); err != nil {
		return nil, err
	}
	out := make([]media.Library, len(answer.MusicFolders.MusicFolder))
	for i, folder := range answer.MusicFolders.MusicFolder {
		out[i] = media.Library{ID: string(folder.ID), Name: folder.Name}
	}
	return out, nil
}

// AllTracks lists every song through search3 with an empty query, a page at
// a time. With no filter it asks once for every library the user can see.
func (c *Client) AllTracks(ctx context.Context, filter string) ([]media.Track, error) {
	folders := []string{""}
	if filter != "" {
		libraries, err := c.Libraries(ctx)
		if err != nil {
			return nil, err
		}
		folders = nil
		for _, library := range libraries {
			if filter == library.ID || filter == library.Name {
				folders = append(folders, library.ID)
			}
		}
		if len(folders) == 0 {
			return nil, fmt.Errorf("navidrome: no music library matches %q", filter)
		}
	}
	var out []media.Track
	for _, folder := range folders {
		songs, err := media.AllPages(c.pageSize,
			func(s Song) string { return s.ID },
			func(start int) ([]Song, error) { return c.songPage(ctx, folder, start) })
		if err != nil {
			return nil, err
		}
		for _, song := range songs {
			if song.audio() {
				out = append(out, song.convert())
			}
		}
	}
	return out, nil
}

func (c *Client) songPage(ctx context.Context, folder string, start int) ([]Song, error) {
	query := url.Values{
		"query":       {`""`},
		"artistCount": {"0"},
		"albumCount":  {"0"},
		"songCount":   {strconv.Itoa(c.pageSize)},
		"songOffset":  {strconv.Itoa(start)},
	}
	if folder != "" {
		query.Set("musicFolderId", folder)
	}
	var answer struct {
		SearchResult3 struct {
			Song []Song `json:"song"`
		} `json:"searchResult3"`
	}
	if err := c.call(ctx, "search3", query, &answer); err != nil {
		return nil, err
	}
	return answer.SearchResult3.Song, nil
}

func (c *Client) Track(ctx context.Context, id string) (media.Track, error) {
	var answer struct {
		Song *Song `json:"song"`
	}
	if err := c.call(ctx, "getSong", url.Values{"id": {id}}, &answer); err != nil {
		return media.Track{}, err
	}
	if answer.Song == nil || !answer.Song.audio() {
		return media.Track{}, media.ErrNotFound
	}
	return answer.Song.convert(), nil
}

func (c *Client) OpenFile(ctx context.Context, track media.Track, method string, header http.Header) (*http.Response, error) {
	return c.open(ctx, method, track.FileRef, header)
}

func (c *Client) OpenArt(ctx context.Context, track media.Track) (*http.Response, error) {
	return c.open(ctx, http.MethodGet, track.ArtRef, nil)
}

// open fetches bytes. A Subsonic server reports a failure as a 200 with a
// JSON or XML body instead of the file, so such an answer is turned into
// the error it carries rather than passed on as audio.
func (c *Client) open(ctx context.Context, method, path string, header http.Header) (*http.Response, error) {
	res, err := c.http.Open(ctx, method, path, header)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK || !isEnvelope(res.Header.Get("Content-Type")) {
		return res, nil
	}
	defer res.Body.Close()
	var answer struct {
		Response json.RawMessage `json:"subsonic-response"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&answer); err != nil || len(answer.Response) == 0 {
		return nil, errors.New("navidrome: answered with a message instead of the file")
	}
	if err := decode(answer.Response, nil); err != nil {
		return nil, err
	}
	return nil, errors.New("navidrome: answered with a message instead of the file")
}

func isEnvelope(contentType string) bool {
	kind, _, _ := mime.ParseMediaType(contentType)
	return kind == "application/json" || kind == "application/xml" || kind == "text/xml" || strings.HasSuffix(kind, "+json")
}
