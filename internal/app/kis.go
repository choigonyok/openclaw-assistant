package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const kisBaseURL = "https://openapi.koreainvestment.com:9443"

type KISClient struct {
	appKey         string
	appSecret      string
	accountNo      string // 계좌번호 앞 8자리
	accountProduct string // 보통 "01"
	isMock         bool

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func NewKISClient(appKey, appSecret, accountNo, accountProduct string, isMock bool) *KISClient {
	if accountProduct == "" {
		accountProduct = "01"
	}
	return &KISClient{
		appKey:         appKey,
		appSecret:      appSecret,
		accountNo:      accountNo,
		accountProduct: accountProduct,
		isMock:         isMock,
	}
}

func (c *KISClient) Enabled() bool {
	return c.appKey != "" && c.appSecret != "" && c.accountNo != ""
}

func (c *KISClient) getToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && time.Now().Before(c.tokenExpiry) {
		return c.accessToken, nil
	}

	body, _ := json.Marshal(map[string]string{
		"grant_type": "client_credentials",
		"appkey":     c.appKey,
		"appsecret":  c.appSecret,
	})
	resp, err := http.Post(kisBaseURL+"/oauth2/tokenP", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("토큰 요청 실패: %w", err)
	}
	defer resp.Body.Close()

	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Message     string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", fmt.Errorf("토큰 응답 파싱 실패: %w", err)
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("토큰 발급 실패: %s", tr.Message)
	}

	c.accessToken = tr.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(tr.ExpiresIn-120) * time.Second)
	return c.accessToken, nil
}

type Holding struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Qty      string `json:"qty"`
	AvgPrice string `json:"avg_price"`
	CurPrice string `json:"cur_price"`
	EvalAmt  string `json:"eval_amt"`
	PnlAmt   string `json:"pnl_amt"`
	PnlRate  string `json:"pnl_rate"`
}

type BalanceSummary struct {
	CashAmt  string `json:"cash_amt"`
	StockAmt string `json:"stock_amt"`
	TotalAmt string `json:"total_amt"`
	BuyAmt   string `json:"buy_amt"`
	PnlAmt   string `json:"pnl_amt"`
}

type BalanceResult struct {
	Holdings []Holding      `json:"holdings"`
	Summary  BalanceSummary `json:"summary"`
}

func (c *KISClient) GetBalance() (*BalanceResult, error) {
	token, err := c.getToken()
	if err != nil {
		return nil, err
	}

	trID := "TTTC8434R"
	if c.isMock {
		trID = "VTTC8434R"
	}

	params := url.Values{}
	params.Set("CANO", c.accountNo)
	params.Set("ACNT_PRDT_CD", c.accountProduct)
	params.Set("AFHR_FLPR_YN", "N")
	params.Set("OFL_YN", "")
	params.Set("INQR_DVSN", "02")
	params.Set("UNPR_DVSN", "01")
	params.Set("FUND_STTL_ICLD_YN", "N")
	params.Set("FNCG_AMT_AUTO_RDPT_YN", "N")
	params.Set("PRCS_DVSN", "00")
	params.Set("CTX_AREA_FK100", "")
	params.Set("CTX_AREA_NK100", "")

	req, err := http.NewRequest("GET", kisBaseURL+"/uapi/domestic-stock/v1/trading/inquire-balance?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("appkey", c.appKey)
	req.Header.Set("appsecret", c.appSecret)
	req.Header.Set("tr_id", trID)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("custtype", "P")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("잔고 조회 요청 실패: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var res struct {
		MsgCode string `json:"msg_cd"`
		Msg1    string `json:"msg1"`
		Output1 []struct {
			Pdno        string `json:"pdno"`
			PrdtName    string `json:"prdt_name"`
			HldgQty     string `json:"hldg_qty"`
			PchsAvgPric string `json:"pchs_avg_pric"`
			Prpr        string `json:"prpr"`
			EvluAmt     string `json:"evlu_amt"`
			EvluPflsAmt string `json:"evlu_pfls_amt"`
			EvluPflsRt  string `json:"evlu_pfls_rt"`
		} `json:"output1"`
		Output2 []struct {
			DncaTotAmt     string `json:"dnca_tot_amt"`
			SctsEvluAmt    string `json:"scts_evlu_amt"`
			TotEvluAmt     string `json:"tot_evlu_amt"`
			PchsAmtSmtlAmt string `json:"pchs_amt_smtl_amt"`
			EvluPflsSmtlAmt string `json:"evlu_pfls_smtl_amt"`
		} `json:"output2"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("응답 파싱 실패: %w", err)
	}
	if res.MsgCode != "" && res.MsgCode != "MCA00000" {
		return nil, fmt.Errorf("API 오류 [%s]: %s", res.MsgCode, res.Msg1)
	}

	result := &BalanceResult{Holdings: []Holding{}}
	for _, h := range res.Output1 {
		if h.HldgQty == "0" || h.HldgQty == "" {
			continue
		}
		result.Holdings = append(result.Holdings, Holding{
			Code:     h.Pdno,
			Name:     h.PrdtName,
			Qty:      h.HldgQty,
			AvgPrice: h.PchsAvgPric,
			CurPrice: h.Prpr,
			EvalAmt:  h.EvluAmt,
			PnlAmt:   h.EvluPflsAmt,
			PnlRate:  h.EvluPflsRt,
		})
	}
	if len(res.Output2) > 0 {
		o := res.Output2[0]
		result.Summary = BalanceSummary{
			CashAmt:  o.DncaTotAmt,
			StockAmt: o.SctsEvluAmt,
			TotalAmt: o.TotEvluAmt,
			BuyAmt:   o.PchsAmtSmtlAmt,
			PnlAmt:   o.EvluPflsSmtlAmt,
		}
	}
	return result, nil
}
