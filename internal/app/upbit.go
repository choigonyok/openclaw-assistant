package app

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const upbitBaseURL = "https://api.upbit.com/v1"

type UpbitClient struct {
	accessKey string
	secretKey string
}

func NewUpbitClient(accessKey, secretKey string) *UpbitClient {
	return &UpbitClient{accessKey: accessKey, secretKey: secretKey}
}

func (c *UpbitClient) Enabled() bool {
	return c.accessKey != "" && c.secretKey != ""
}

func (c *UpbitClient) makeJWT() (string, error) {
	headerJSON, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	header := base64.RawURLEncoding.EncodeToString(headerJSON)

	payloadJSON, _ := json.Marshal(map[string]string{
		"access_key": c.accessKey,
		"nonce":      upbitUUID(),
	})
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)

	sigInput := header + "." + payload
	mac := hmac.New(sha256.New, []byte(c.secretKey))
	mac.Write([]byte(sigInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return sigInput + "." + sig, nil
}

func upbitUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

type CryptoAsset struct {
	Currency    string `json:"currency"`
	Balance     string `json:"balance"`
	AvgBuyPrice string `json:"avg_buy_price"`
	CurPrice    string `json:"cur_price"`
	EvalAmt     string `json:"eval_amt"`
	PnlAmt      string `json:"pnl_amt"`
	PnlRate     string `json:"pnl_rate"`
}

type CryptoResult struct {
	KRWBalance string        `json:"krw_balance"`
	TotalEval  string        `json:"total_eval"`
	TotalPnl   string        `json:"total_pnl"`
	Assets     []CryptoAsset `json:"assets"`
}

func (c *UpbitClient) GetAssets() (*CryptoResult, error) {
	token, err := c.makeJWT()
	if err != nil {
		return nil, err
	}

	req, _ := http.NewRequest("GET", upbitBaseURL+"/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("계좌 조회 실패: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("계좌 조회 실패: %s", upbitErrorMessage(resp))
	}

	var accounts []struct {
		Currency     string `json:"currency"`
		Balance      string `json:"balance"`
		Locked       string `json:"locked"`
		AvgBuyPrice  string `json:"avg_buy_price"`
		UnitCurrency string `json:"unit_currency"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&accounts); err != nil {
		return nil, fmt.Errorf("응답 파싱 실패: %w", err)
	}

	result := &CryptoResult{Assets: []CryptoAsset{}}

	// KRW-마켓 코인 목록 수집
	type coinInfo struct {
		currency string
		total    float64
		avg      float64
	}
	validKRWMarkets := fetchUpbitKRWMarkets()
	var coins []coinInfo
	var markets []string

	for _, acc := range accounts {
		bal, _ := strconv.ParseFloat(acc.Balance, 64)
		locked, _ := strconv.ParseFloat(acc.Locked, 64)
		total := bal + locked

		if acc.Currency == "KRW" {
			result.KRWBalance = fmt.Sprintf("%.0f", total)
			continue
		}
		if total <= 0 {
			continue
		}
		avg, _ := strconv.ParseFloat(acc.AvgBuyPrice, 64)
		coins = append(coins, coinInfo{acc.Currency, total, avg})
		market := "KRW-" + acc.Currency
		if validKRWMarkets[market] {
			markets = append(markets, market)
		}
	}

	// 현재가 일괄 조회
	prices := map[string]float64{}
	if len(markets) > 0 {
		tickerURL := upbitBaseURL + "/ticker?markets=" + url.QueryEscape(strings.Join(markets, ","))
		tr, err := http.Get(tickerURL)
		if err == nil {
			defer tr.Body.Close()
			if tr.StatusCode < 200 || tr.StatusCode >= 300 {
				return nil, fmt.Errorf("현재가 조회 실패: %s", upbitErrorMessage(tr))
			}
			var tickers []struct {
				Market     string  `json:"market"`
				TradePrice float64 `json:"trade_price"`
			}
			if json.NewDecoder(tr.Body).Decode(&tickers) == nil {
				for _, t := range tickers {
					prices[t.Market] = t.TradePrice
				}
			}
		}
	}

	var totalEval, totalPnl float64
	if krw, err := strconv.ParseFloat(result.KRWBalance, 64); err == nil {
		totalEval += krw
	}

	for _, coin := range coins {
		cur := prices["KRW-"+coin.currency]
		evalAmt := coin.total * cur
		buyAmt := coin.total * coin.avg
		pnlAmt := evalAmt - buyAmt
		var pnlRate float64
		if buyAmt > 0 {
			pnlRate = (pnlAmt / buyAmt) * 100
		}
		totalEval += evalAmt
		totalPnl += pnlAmt

		balStr := fmt.Sprintf("%g", coin.total)
		result.Assets = append(result.Assets, CryptoAsset{
			Currency:    coin.currency,
			Balance:     balStr,
			AvgBuyPrice: fmt.Sprintf("%.0f", coin.avg),
			CurPrice:    fmt.Sprintf("%.0f", cur),
			EvalAmt:     fmt.Sprintf("%.0f", evalAmt),
			PnlAmt:      fmt.Sprintf("%.0f", math.Round(pnlAmt)),
			PnlRate:     fmt.Sprintf("%.2f", pnlRate),
		})
	}

	result.TotalEval = fmt.Sprintf("%.0f", totalEval)
	result.TotalPnl = fmt.Sprintf("%.0f", math.Round(totalPnl))
	return result, nil
}

func fetchUpbitKRWMarkets() map[string]bool {
	markets := map[string]bool{}
	resp, err := http.Get(upbitBaseURL + "/market/all")
	if err != nil {
		return markets
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return markets
	}

	var listed []struct {
		Market string `json:"market"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		return markets
	}
	for _, item := range listed {
		if strings.HasPrefix(item.Market, "KRW-") {
			markets[item.Market] = true
		}
	}
	return markets
}

func upbitErrorMessage(resp *http.Response) string {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var body struct {
		Error struct {
			Name    interface{} `json:"name"`
			Message string      `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &body); err == nil && body.Error.Message != "" {
		return body.Error.Message
	}
	return strings.TrimSpace(string(raw))
}
