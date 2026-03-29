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

// fetchAndPrint fetches data from the API and prints it as indented JSON to stdout.
func (c *CLI) fetchAndPrint(path string, params url.Values) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.config.GetTimeout())
	defer cancel()

	if c.config.GetAPIKey() == "" {
		return fmt.Errorf("API key is required. Set it via --api-key flag or QF_API_KEY environment variable")
	}

	data, err := c.apiClient.Get(ctx, path, params)
	if err != nil {
		return err
	}

	// Pretty-print JSON
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, "", "  "); err != nil {
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	}

	fmt.Fprintln(os.Stdout, buf.String())
	return nil
}

// addListFlags adds common list flags (page, sort-by, sort-desc) to a command's params builder.
func addPagination(params url.Values, page int) {
	if page > 0 {
		params.Set("page", strconv.Itoa(page))
	}
}

func addSorting(params url.Values, sortBy string, sortDesc bool) {
	if sortBy != "" {
		params.Set("sortBy", sortBy)
	}
	if sortDesc {
		params.Set("sortDir", "desc")
	}
}

func addSliceParam(params url.Values, key string, values []string) {
	for _, v := range values {
		if t := strings.TrimSpace(v); t != "" {
			params.Add(key, t)
		}
	}
}

