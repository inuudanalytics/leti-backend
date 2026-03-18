package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

type PaystackClient struct {
	SecretKey string
	BaseURL   string
	Client    *http.Client
}

func NewPaystackClient() (*PaystackClient, error) {
	secretKey := os.Getenv("PAYSTACK_SECRET_KEY")
	if secretKey == "" {
		return nil, fmt.Errorf("PAYSTACK_SECRET_KEY environment variable is not set")
	}
	return &PaystackClient{
		SecretKey: secretKey,
		BaseURL:   "https://api.paystack.co",
		Client:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

type PaystackResponse struct {
	Status  bool        `json:"status"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

func (p *PaystackClient) doRequest(method, endpoint string, body interface{}) (*PaystackResponse, error) {
	endpointURL := fmt.Sprintf("%s%s", p.BaseURL, endpoint)
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(data)
	}

	req, err := http.NewRequest(method, endpointURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Add("Authorization", "Bearer "+p.SecretKey)
	req.Header.Add("Content-Type", "application/json")

	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(respBody))
	}

	var res PaystackResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("failed to decode response (status %d): %w", resp.StatusCode, err)
	}

	if !res.Status {
		return nil, fmt.Errorf("API error: %s", res.Message)
	}

	return &res, nil
}

func (p *PaystackClient) InitializePayment(form map[string]interface{}) (*PaystackResponse, error) {
	requiredFields := []string{"amount", "email"}
	for _, field := range requiredFields {
		if _, ok := form[field]; !ok {
			return nil, fmt.Errorf("missing required field: %s", field)
		}
	}
	return p.doRequest("POST", "/transaction/initialize", form)
}

func (p *PaystackClient) VerifyPayment(ref string) (*PaystackResponse, error) {
	if ref == "" {
		return nil, fmt.Errorf("reference cannot be empty")
	}
	escapedRef := url.PathEscape(ref)
	return p.doRequest("GET", fmt.Sprintf("/transaction/verify/%s", escapedRef), nil)
}

func (p *PaystackClient) CreateRecipient(form map[string]interface{}) (*PaystackResponse, error) {
	requiredFields := []string{"type", "name", "account_number", "bank_code"}
	for _, field := range requiredFields {
		if _, ok := form[field]; !ok {
			return nil, fmt.Errorf("missing required field: %s", field)
		}
	}
	return p.doRequest("POST", "/transferrecipient", form)
}

func (p *PaystackClient) InitiateTransfer(form map[string]interface{}) (*PaystackResponse, error) {
	requiredFields := []string{"source", "amount", "recipient"}
	for _, field := range requiredFields {
		if _, ok := form[field]; !ok {
			return nil, fmt.Errorf("missing required field: %s", field)
		}
	}
	return p.doRequest("POST", "/transfer", form)
}

func (p *PaystackClient) InitiateBulkTransfer(transfers []map[string]interface{}) (*PaystackResponse, error) {
	if len(transfers) == 0 {
		return nil, fmt.Errorf("transfers list cannot be empty")
	}
	if len(transfers) > 100 {
		return nil, fmt.Errorf("maximum of 100 transfers per batch")
	}
	return p.doRequest("POST", "/transfer/bulk", map[string]interface{}{
		"currency":  "NGN",
		"source":    "balance",
		"transfers": transfers,
	})
}

func (p *PaystackClient) FinalizeTransfer(transferCode, otp string) (*PaystackResponse, error) {
	if transferCode == "" {
		return nil, fmt.Errorf("transfer_code cannot be empty")
	}
	if otp == "" {
		return nil, fmt.Errorf("otp cannot be empty")
	}
	return p.doRequest("POST", "/transfer/finalize_transfer", map[string]interface{}{
		"transfer_code": transferCode,
		"otp":           otp,
	})
}

func (p *PaystackClient) VerifyTransfer(reference string) (*PaystackResponse, error) {
	if reference == "" {
		return nil, fmt.Errorf("reference cannot be empty")
	}
	escapedRef := url.PathEscape(reference)
	return p.doRequest("GET", fmt.Sprintf("/transfer/verify/%s", escapedRef), nil)
}

func (p *PaystackClient) VerifyBankDetails(accountNumber, bankName string) (*PaystackResponse, error) {
	match, _ := regexp.MatchString(`^\d{10}$`, accountNumber)
	if !match {
		return nil, fmt.Errorf("invalid account number: must be 10 digits")
	}

	banksRes, err := p.doRequest("GET", "/bank", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch banks: %w", err)
	}

	bankList, ok := banksRes.Data.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response format from Paystack /bank")
	}

	var bankCode string
	for _, b := range bankList {
		bankMap, ok := b.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := bankMap["name"].(string)
		code, _ := bankMap["code"].(string)
		if strings.EqualFold(name, bankName) {
			bankCode = code
			break
		}
	}

	if bankCode == "" {
		return nil, fmt.Errorf("bank not found or unsupported: %s", bankName)
	}

	endpoint := fmt.Sprintf("/bank/resolve?account_number=%s&bank_code=%s", accountNumber, url.QueryEscape(bankCode))
	verifyRes, err := p.doRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to verify account details: %w", err)
	}

	dataMap, ok := verifyRes.Data.(map[string]interface{})

	if !ok {
		return nil, fmt.Errorf("unexpected verify response format")
	}

	accountName, _ := dataMap["account_name"].(string)

	return &PaystackResponse{
		Status:  true,
		Message: "Account verified successfully",
		Data: map[string]interface{}{
			"accountName":   accountName,
			"bankCode":      bankCode,
			"accountNumber": accountNumber,
			"bankName":      bankName,
		},
	}, nil
}

func (p *PaystackClient) GetBanks() (*PaystackResponse, error) {
	return p.doRequest("GET", "/bank?currency=NGN&use_cursor=false&perPage=100", nil)
}
