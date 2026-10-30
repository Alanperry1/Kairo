package research

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

type DatasetCandidate struct {
	Name        string `json:"name"`
	URL         string `json:"url,omitempty"`
	PageURL     string `json:"page_url,omitempty"`
	Publisher   string `json:"publisher,omitempty"`
	License     string `json:"license,omitempty"`
	Description string `json:"description,omitempty"`
	DatasetID   string `json:"dataset_id,omitempty"`
}
type DatasetDiscovery struct {
	Workspace []Dataset          `json:"workspace"`
	Public    []DatasetCandidate `json:"public"`
	Warnings  []string           `json:"warnings,omitempty"`
}

func publicIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsUnspecified() && !ip.IsMulticast() && ip.IsGlobalUnicast() && !(&net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}).Contains(ip)
}
func validDatasetURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") || len(raw) > 4000 {
		return nil, errors.New("use a public HTTPS dataset URL without credentials")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !publicIP(ip) {
		return nil, errors.New("private network dataset URLs are not allowed")
	}
	return u, nil
}
func publicDatasetClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ResponseHeaderTimeout: 20 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, errors.New("dataset host could not be resolved")
		}
		if len(ips) == 0 {
			return nil, errors.New("dataset host has no public address")
		}
		for _, item := range ips {
			if !publicIP(item.IP) {
				return nil, errors.New("private network dataset addresses are not allowed")
			}
		}
		dialer := net.Dialer{Timeout: 15 * time.Second}
		var last error
		for _, item := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(item.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}}
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("too many dataset redirects")
		}
		_, err := validDatasetURL(r.URL.String())
		return err
	}}
}
func fetchPublic(ctx context.Context, client *http.Client, raw string) (*http.Response, error) {
	if _, err := validDatasetURL(raw); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Kairo-Research/1.0")
	req.Header.Set("Accept", "text/csv, application/json;q=0.9, */*;q=0.5")
	return client.Do(req)
}
func searchCKAN(ctx context.Context, endpoint, query, publisher string) ([]DatasetCandidate, error) {
	u, _ := url.Parse(endpoint)
	values := u.Query()
	values.Set("q", query)
	values.Set("rows", "8")
	values.Set("fq", "res_format:CSV")
	u.RawQuery = values.Encode()
	response, err := fetchPublic(ctx, publicDatasetClient(12*time.Second), u.String())
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("catalog returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Success bool `json:"success"`
		Result  struct {
			Results []struct {
				Title     string `json:"title"`
				Notes     string `json:"notes"`
				License   string `json:"license_title"`
				Resources []struct {
					Name   string `json:"name"`
					Format string `json:"format"`
					URL    string `json:"url"`
				} `json:"resources"`
			} `json:"results"`
		} `json:"result"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 3<<20)).Decode(&payload); err != nil || !payload.Success {
		return nil, errors.New("catalog search unavailable")
	}
	results := []DatasetCandidate{}
	for _, dataset := range payload.Result.Results {
		for _, resource := range dataset.Resources {
			if !strings.EqualFold(resource.Format, "csv") {
				continue
			}
			if _, err := validDatasetURL(resource.URL); err != nil {
				continue
			}
			name := dataset.Title
			if resource.Name != "" {
				name += " · " + resource.Name
			}
			if len(name) > 240 {
				name = name[:240]
			}
			description := dataset.Notes
			if len(description) > 600 {
				description = description[:600]
			}
			results = append(results, DatasetCandidate{Name: name, URL: resource.URL, Publisher: publisher, License: dataset.License, Description: description})
			if len(results) >= 8 {
				return results, nil
			}
		}
	}
	return results, nil
}
func (s *Server) discoverDatasets(ctx context.Context, query string) (DatasetDiscovery, error) {
	query = strings.TrimSpace(query)
	if len(query) < 2 || len(query) > 300 {
		return DatasetDiscovery{}, errors.New("search must contain 2–300 characters")
	}
	out := DatasetDiscovery{Workspace: []Dataset{}, Public: []DatasetCandidate{}}
	if s.Store != nil {
		datasets, err := s.Store.Datasets()
		if err != nil {
			return out, err
		}
		for _, d := range datasets {
			if strings.Contains(strings.ToLower(d.Name+" "+strings.Join(d.Columns, " ")), strings.ToLower(query)) {
				out.Workspace = append(out.Workspace, d)
				if len(out.Workspace) >= 20 {
					break
				}
			}
		}
	}
	type result struct {
		items []DatasetCandidate
		err   error
		name  string
	}
	results := make(chan result, 2)
	for _, catalog := range [][3]string{{"https://data.gov.uk/api/action/package_search", "UK open data", query}, {"https://data.ca.gov/api/3/action/package_search", "California open data", query}} {
		go func(c [3]string) {
			items, err := searchCKAN(ctx, c[0], c[2], c[1])
			results <- result{items, err, c[1]}
		}(catalog)
	}
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err != nil {
			out.Warnings = append(out.Warnings, r.name+" search is unavailable.")
			continue
		}
		for _, item := range r.items {
			if !seen[item.URL] {
				seen[item.URL] = true
				out.Public = append(out.Public, item)
			}
		}
	}
	return out, nil
}
func (s *Server) searchDatasets(w http.ResponseWriter, r *http.Request) {
	out, err := s.discoverDatasets(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		apiError(w, 422, "invalid_dataset_search", err.Error())
		return
	}
	respond(w, 200, out)
}
func (s *Server) importDataset(w http.ResponseWriter, r *http.Request) {
	longDatasetRequest(w, r)
	var req struct {
		URL  string `json:"url"`
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	parsed, err := validDatasetURL(req.URL)
	if err != nil {
		apiError(w, 422, "invalid_dataset_url", err.Error())
		return
	}
	name := req.Name
	if name == "" {
		name = path.Base(parsed.Path)
	}
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	if !strings.HasSuffix(strings.ToLower(name), ".csv") {
		name += ".csv"
	}
	response, err := fetchPublic(r.Context(), publicDatasetClient(6*time.Hour), req.URL)
	if err != nil {
		apiError(w, 422, "dataset_fetch_failed", "Could not download the public dataset. Check that the HTTPS URL points to an accessible CSV file.")
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		apiError(w, 422, "dataset_fetch_failed", fmt.Sprintf("Dataset download returned HTTP %d", response.StatusCode))
		return
	}
	if response.ContentLength > MaxDatasetBytes {
		apiError(w, 413, "dataset_too_large", errDatasetTooLarge.Error())
		return
	}
	dataset, err := s.ingestDataset(r.Context(), name, response.Body, req.URL)
	if err != nil {
		status := 422
		if errors.Is(err, errDatasetTooLarge) {
			status = 413
		}
		apiError(w, status, "dataset_import_failed", err.Error())
		return
	}
	respond(w, 201, dataset)
}

var datasetURLInMessage = regexp.MustCompile(`https://[^\s<>"\x60]+`)
