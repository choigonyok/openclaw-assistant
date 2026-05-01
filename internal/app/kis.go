package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
	accountNo, accountProduct = normalizeKISAccount(accountNo, accountProduct)
	return &KISClient{
		appKey:         appKey,
		appSecret:      appSecret,
		accountNo:      accountNo,
		accountProduct: accountProduct,
		isMock:         isMock,
	}
}

func normalizeKISAccount(accountNo, accountProduct string) (string, string) {
	accountNo = strings.TrimSpace(accountNo)
	accountProduct = strings.TrimSpace(accountProduct)
	compact := strings.NewReplacer("-", "", " ", "").Replace(accountNo)

	if len(compact) >= 10 {
		if accountProduct == "" {
			accountProduct = compact[8:10]
		}
		compact = compact[:8]
	}
	if accountProduct == "" {
		accountProduct = "01"
	}
	return compact, accountProduct
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
	CashAmt    string `json:"cash_amt"`
	CashKRW    string `json:"cash_krw"`
	CashUSD    string `json:"cash_usd"`
	CashUSDKRW string `json:"cash_usd_krw"`
	StockAmt   string `json:"stock_amt"`
	TotalAmt   string `json:"total_amt"`
	BuyAmt     string `json:"buy_amt"`
	PnlAmt     string `json:"pnl_amt"`
}

type KISDiagnostics struct {
	DomesticTRID        string `json:"domestic_tr_id,omitempty"`
	DomesticOutput2Rows int    `json:"domestic_output2_rows"`
	DomesticCashTRID    string `json:"domestic_cash_tr_id,omitempty"`
	DomesticCashMsgCode string `json:"domestic_cash_msg_code,omitempty"`
	DomesticCashMsg     string `json:"domestic_cash_msg,omitempty"`
	DomesticCashError   string `json:"domestic_cash_error,omitempty"`
	ForeignTRID         string `json:"foreign_tr_id,omitempty"`
	ForeignMsgCode      string `json:"foreign_msg_code,omitempty"`
	ForeignMsg          string `json:"foreign_msg,omitempty"`
	ForeignOutput2Rows  int    `json:"foreign_output2_rows"`
	ForeignOutput3Rows  int    `json:"foreign_output3_rows"`
	ForeignError        string `json:"foreign_error,omitempty"`
}

type BalanceResult struct {
	Holdings    []Holding      `json:"holdings"`
	Summary     BalanceSummary `json:"summary"`
	Diagnostics KISDiagnostics `json:"diagnostics"`
}

type kisForeignCashRow struct {
	Currency       string `json:"crcy_cd"`
	ForeignCash    string `json:"frcr_dncl_amt_2"`
	ForeignUsable  string `json:"frcr_use_psbl_amt"`
	ForeignBalance string `json:"tot_frcr_cblc_smtl"`
	DepositAmount  string `json:"dncl_amt"`
	TotalDeposit   string `json:"tot_dncl_amt"`
	ForeignKRW     string `json:"frcr_evlu_amt2"`
	BaseRate       string `json:"bass_exrt"`
}

type kisForeignCashRows []kisForeignCashRow

func (r *kisForeignCashRows) UnmarshalJSON(data []byte) error {
	var rows []kisForeignCashRow
	if err := json.Unmarshal(data, &rows); err == nil {
		*r = rows
		return nil
	}

	var row kisForeignCashRow
	if err := json.Unmarshal(data, &row); err != nil {
		return err
	}
	*r = []kisForeignCashRow{row}
	return nil
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("잔고 조회 HTTP 오류 [%d]: %s", resp.StatusCode, compactBody(raw))
	}

	var res struct {
		RtCode  string `json:"rt_cd"`
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
			DncaTotAmt      string `json:"dnca_tot_amt"`
			SctsEvluAmt     string `json:"scts_evlu_amt"`
			TotEvluAmt      string `json:"tot_evlu_amt"`
			PchsAmtSmtlAmt  string `json:"pchs_amt_smtl_amt"`
			EvluPflsSmtlAmt string `json:"evlu_pfls_smtl_amt"`
		} `json:"output2"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("응답 파싱 실패: %w", err)
	}
	if res.RtCode != "" && res.RtCode != "0" {
		return nil, fmt.Errorf("API 오류 [%s]: %s", res.MsgCode, res.Msg1)
	}

	result := &BalanceResult{
		Holdings: []Holding{},
		Diagnostics: KISDiagnostics{
			DomesticTRID:        trID,
			DomesticOutput2Rows: len(res.Output2),
		},
	}
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
			CashKRW:  o.DncaTotAmt,
			StockAmt: o.SctsEvluAmt,
			TotalAmt: o.TotEvluAmt,
			BuyAmt:   o.PchsAmtSmtlAmt,
			PnlAmt:   o.EvluPflsSmtlAmt,
		}
	}
	domesticCash := c.getKRWOrderableCash(token)
	result.Diagnostics.DomesticCashTRID = domesticCash.trID
	result.Diagnostics.DomesticCashMsgCode = domesticCash.msgCode
	result.Diagnostics.DomesticCashMsg = domesticCash.msg
	if domesticCash.err != nil {
		result.Diagnostics.DomesticCashError = domesticCash.err.Error()
	}
	if domesticCash.cash != "" {
		result.Summary.CashKRW = domesticCash.cash
		result.Summary.CashAmt = domesticCash.cash
	}
	foreign := c.getUSDCash(token)
	result.Diagnostics.ForeignTRID = foreign.trID
	result.Diagnostics.ForeignMsgCode = foreign.msgCode
	result.Diagnostics.ForeignMsg = foreign.msg
	result.Diagnostics.ForeignOutput2Rows = foreign.output2Rows
	result.Diagnostics.ForeignOutput3Rows = foreign.output3Rows
	if foreign.err != nil {
		result.Diagnostics.ForeignError = foreign.err.Error()
	}
	result.Summary.CashUSD = foreign.cash
	result.Summary.CashUSDKRW = foreign.krw
	return result, nil
}

type kisForeignCashResult struct {
	cash        string
	krw         string
	trID        string
	msgCode     string
	msg         string
	output2Rows int
	output3Rows int
	err         error
}

type kisKRWCashResult struct {
	cash    string
	trID    string
	msgCode string
	msg     string
	err     error
}

func (c *KISClient) getKRWOrderableCash(token string) kisKRWCashResult {
	trID := "TTTC8908R"
	if c.isMock {
		trID = "VTTC8908R"
	}
	result := kisKRWCashResult{trID: trID}

	params := url.Values{}
	params.Set("CANO", c.accountNo)
	params.Set("ACNT_PRDT_CD", c.accountProduct)
	params.Set("PDNO", "005930")
	params.Set("ORD_UNPR", "1")
	params.Set("ORD_DVSN", "01")
	params.Set("CMA_EVLU_AMT_ICLD_YN", "Y")
	params.Set("OVRS_ICLD_YN", "N")

	req, err := http.NewRequest("GET", kisBaseURL+"/uapi/domestic-stock/v1/trading/inquire-psbl-order?"+params.Encode(), nil)
	if err != nil {
		result.err = err
		return result
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("appkey", c.appKey)
	req.Header.Set("appsecret", c.appSecret)
	req.Header.Set("tr_id", trID)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("custtype", "P")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		result.err = fmt.Errorf("원화 매수가능금액 조회 요청 실패: %w", err)
		return result
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		result.err = fmt.Errorf("원화 매수가능금액 응답 읽기 실패: %w", err)
		return result
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.err = fmt.Errorf("원화 매수가능금액 HTTP 오류 [%d]: %s", resp.StatusCode, compactBody(raw))
		return result
	}

	var res struct {
		RtCode  string `json:"rt_cd"`
		MsgCode string `json:"msg_cd"`
		Msg1    string `json:"msg1"`
		Output  struct {
			OrderableCash string `json:"ord_psbl_cash"`
			NoCreditBuy   string `json:"nrcvb_buy_amt"`
			MaxBuy        string `json:"max_buy_amt"`
			CMAValue      string `json:"cma_evlu_amt"`
		} `json:"output"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		result.err = fmt.Errorf("원화 매수가능금액 응답 파싱 실패: %w", err)
		return result
	}
	result.msgCode = res.MsgCode
	result.msg = res.Msg1
	if res.RtCode != "" && res.RtCode != "0" {
		result.err = fmt.Errorf("원화 매수가능금액 API 오류 [%s]: %s", res.MsgCode, res.Msg1)
		return result
	}
	result.cash = firstNonZero(res.Output.OrderableCash, res.Output.NoCreditBuy, res.Output.MaxBuy, res.Output.CMAValue)
	if result.cash == "" {
		result.err = fmt.Errorf("원화 매수가능금액 응답에 사용할 현금 필드가 없습니다")
	}
	return result
}

func (c *KISClient) getUSDCash(token string) kisForeignCashResult {
	trID := "CTRP6504R"
	if c.isMock {
		trID = "VTRP6504R"
	}
	result := kisForeignCashResult{trID: trID}

	params := url.Values{}
	params.Set("CANO", c.accountNo)
	params.Set("ACNT_PRDT_CD", c.accountProduct)
	params.Set("WCRC_FRCR_DVSN_CD", "02")
	params.Set("NATN_CD", "000")
	params.Set("TR_MKET_CD", "00")
	params.Set("INQR_DVSN_CD", "00")

	req, err := http.NewRequest("GET", kisBaseURL+"/uapi/overseas-stock/v1/trading/inquire-present-balance?"+params.Encode(), nil)
	if err != nil {
		result.err = err
		return result
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("appkey", c.appKey)
	req.Header.Set("appsecret", c.appSecret)
	req.Header.Set("tr_id", trID)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("custtype", "P")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		result.err = fmt.Errorf("외화 예수금 조회 요청 실패: %w", err)
		return result
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		result.err = fmt.Errorf("외화 예수금 응답 읽기 실패: %w", err)
		return result
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.err = fmt.Errorf("외화 예수금 HTTP 오류 [%d]: %s", resp.StatusCode, compactBody(raw))
		return result
	}

	var res struct {
		RtCode  string             `json:"rt_cd"`
		MsgCode string             `json:"msg_cd"`
		Msg1    string             `json:"msg1"`
		Output2 kisForeignCashRows `json:"output2"`
		Output3 kisForeignCashRows `json:"output3"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		result.err = fmt.Errorf("외화 예수금 응답 파싱 실패: %w", err)
		return result
	}
	result.msgCode = res.MsgCode
	result.msg = res.Msg1
	result.output2Rows = len(res.Output2)
	result.output3Rows = len(res.Output3)
	if res.RtCode != "" && res.RtCode != "0" {
		result.err = fmt.Errorf("외화 예수금 API 오류 [%s]: %s", res.MsgCode, res.Msg1)
		return result
	}
	result.cash, result.krw = pickUSDCash(append(res.Output2, res.Output3...))
	if result.cash == "" && result.output2Rows+result.output3Rows == 0 {
		result.err = fmt.Errorf("외화 예수금 응답에 output2/output3 행이 없습니다")
	}
	return result
}

func pickUSDCash(rows []kisForeignCashRow) (string, string) {
	for _, row := range rows {
		if strings.EqualFold(row.Currency, "USD") {
			return row.cashAndKRW()
		}
	}
	for _, row := range rows {
		cash, krw := row.cashAndKRW()
		if cash != "" {
			return cash, krw
		}
	}
	return "", ""
}

func (r kisForeignCashRow) cashAndKRW() (string, string) {
	cash := firstNonZero(r.ForeignCash, r.ForeignUsable, r.ForeignBalance, r.DepositAmount, r.TotalDeposit)
	krw := firstNonEmpty(r.ForeignKRW)
	if krw == "" {
		amount, amountOK := parseKISFloat(cash)
		rate, rateOK := parseKISFloat(r.BaseRate)
		if amountOK && rateOK {
			krw = fmt.Sprintf("%.0f", amount*rate)
		}
	}
	return cash, krw
}

func parseKISFloat(value string) (float64, bool) {
	value = strings.ReplaceAll(strings.TrimSpace(value), ",", "")
	if value == "" {
		return 0, false
	}
	n, err := strconv.ParseFloat(value, 64)
	return n, err == nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstNonZero(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if amount, ok := parseKISFloat(trimmed); ok && amount == 0 {
			continue
		}
		return trimmed
	}
	return firstNonEmpty(values...)
}

func compactBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 500 {
		return text[:500] + "..."
	}
	return text
}
