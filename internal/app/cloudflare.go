package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type CloudflareClient struct {
	apiToken   string
	httpClient *http.Client
}

func NewCloudflareClient(apiToken string) *CloudflareClient {
	return &CloudflareClient{
		apiToken:   apiToken,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *CloudflareClient) Enabled() bool {
	return c.apiToken != ""
}

type CFZone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Plan   struct {
		Name string `json:"name"`
	} `json:"plan"`
}

func (c *CloudflareClient) ListZones(ctx context.Context) ([]CFZone, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.cloudflare.com/client/v4/zones?per_page=50", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out struct {
		Success bool     `json:"success"`
		Result  []CFZone `json:"result"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !out.Success && len(out.Errors) > 0 {
		return nil, fmt.Errorf("cloudflare: %s", out.Errors[0].Message)
	}
	return out.Result, nil
}

type ZoneStats struct {
	RequestsToday  int64
	PageViewsToday int64
	UniquesToday   int64
	BandwidthToday int64
	Requests7d     int64
	PageViews7d    int64
	Uniques7d      int64
	Bandwidth7d    int64
}

func (c *CloudflareClient) GetZoneStats(ctx context.Context, zoneID string) (*ZoneStats, error) {
	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	sevenDaysAgo := now.AddDate(0, 0, -6).Format("2006-01-02")

	query := fmt.Sprintf(
		`{viewer{zones(filter:{zoneTag:%q}){httpRequests1dGroups(limit:7 orderBy:[date_DESC] filter:{date_geq:%q,date_leq:%q}){dimensions{date}sum{requests pageViews bytes}uniq{uniques}}}}}`,
		zoneID, sevenDaysAgo, today,
	)

	body, _ := json.Marshal(map[string]string{"query": query})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.cloudflare.com/client/v4/graphql", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out struct {
		Data struct {
			Viewer struct {
				Zones []struct {
					Groups []struct {
						Dimensions struct {
							Date string `json:"date"`
						} `json:"dimensions"`
						Sum struct {
							Requests  int64 `json:"requests"`
							PageViews int64 `json:"pageViews"`
							Bytes     int64 `json:"bytes"`
						} `json:"sum"`
						Uniq struct {
							Uniques int64 `json:"uniques"`
						} `json:"uniq"`
					} `json:"httpRequests1dGroups"`
				} `json:"zones"`
			} `json:"viewer"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Errors) > 0 {
		return nil, fmt.Errorf("cloudflare graphql: %s", out.Errors[0].Message)
	}
	if len(out.Data.Viewer.Zones) == 0 {
		return &ZoneStats{}, nil
	}

	stats := &ZoneStats{}
	for _, g := range out.Data.Viewer.Zones[0].Groups {
		stats.Requests7d += g.Sum.Requests
		stats.PageViews7d += g.Sum.PageViews
		stats.Uniques7d += g.Uniq.Uniques
		stats.Bandwidth7d += g.Sum.Bytes
		if g.Dimensions.Date == today {
			stats.RequestsToday = g.Sum.Requests
			stats.PageViewsToday = g.Sum.PageViews
			stats.UniquesToday = g.Uniq.Uniques
			stats.BandwidthToday = g.Sum.Bytes
		}
	}
	return stats, nil
}

type CFDNSRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
}

func (c *CloudflareClient) ListDNSRecords(ctx context.Context, zoneID string) ([]CFDNSRecord, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.cloudflare.com/client/v4/zones/"+zoneID+"/dns_records?type=A,CNAME&per_page=100", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out struct {
		Success bool          `json:"success"`
		Result  []CFDNSRecord `json:"result"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !out.Success && len(out.Errors) > 0 {
		return nil, fmt.Errorf("cloudflare dns: %s", out.Errors[0].Message)
	}
	return out.Result, nil
}

type SiteHealth struct {
	Status     string
	HTTPStatus int
	ResponseMs int64
}

func CheckSiteHealth(domain string) SiteHealth {
	client := &http.Client{
		Timeout: 8 * time.Second,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	start := time.Now()
	resp, err := client.Get("https://" + domain)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		return SiteHealth{Status: "down", ResponseMs: ms}
	}
	defer resp.Body.Close()
	status := "up"
	if resp.StatusCode >= 500 {
		status = "down"
	} else if resp.StatusCode >= 400 {
		status = "degraded"
	}
	return SiteHealth{Status: status, HTTPStatus: resp.StatusCode, ResponseMs: ms}
}
