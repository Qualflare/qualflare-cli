package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// pathArg escapes a user-supplied path segment so metacharacters (?, #, /, "..")
// cannot alter the request path or query when interpolated into a URL (SEC-03).
func pathArg(s string) string {
	return url.PathEscape(s)
}

// fetchAndPrint fetches data from the API and prints it as indented JSON to stdout.
func (c *CLI) fetchAndPrint(path string, params url.Values) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.config.GetTimeout())
	defer cancel()

	if c.config.GetAPIKey() == "" {
		return fmt.Errorf("no token loaded; run 'qf login <identifier> <token>' first")
	}

	data, err := c.apiClient.Get(ctx, path, params)
	if err != nil {
		return err
	}

	// Pretty-print JSON
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, "", "  "); err != nil {
		_, _ = fmt.Fprintln(os.Stdout, string(data))
		return nil
	}

	_, _ = fmt.Fprintln(os.Stdout, buf.String())
	return nil
}

// addListFlags adds common list flags (page, sort-by, sort-desc) to a command's params builder.
func addPagination(params url.Values, page int) {
	if page > 0 {
		params.Set("page", strconv.Itoa(page))
	}
}

// addSorting adds the sortBy/sortDir query params. sortDir is sent ONLY when the user
// explicitly passed --sort-desc (sortDirSet). Previously it was always sent as
// false → every list was pinned ascending, so the server could never apply its
// newest-first default for launches/defects (API-02). Omitting it lets the server's
// per-endpoint default direction take effect.
func addSorting(params url.Values, sortBy string, sortDesc, sortDirSet bool) {
	if sortBy != "" {
		params.Set("sortBy", sortBy)
	}
	if sortDirSet {
		params.Set("sortDir", strconv.FormatBool(sortDesc))
	}
}

func addSliceParam(params url.Values, key string, values []string) {
	for _, v := range values {
		if t := strings.TrimSpace(v); t != "" {
			params.Add(key, t)
		}
	}
}
