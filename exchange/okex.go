package exchange

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/polyrabbit/token-ticker/exchange/model"

	"github.com/polyrabbit/token-ticker/http"
	"github.com/sirupsen/logrus"
)

// https://github.com/okcoin-okex/API-docs-OKEx.com
const okexBaseApi = "https://www.okex.com/api/v1"

type okexClient struct {
	AccessKey string
	SecretKey string
}

type okexErrorResponse struct {
	ErrorCode int `json:"error_code"`
}

type okexTickerResponse struct {
	okexErrorResponse
	Date   int64 `json:",string"`
	Ticker struct {
		Last float64 `json:",string"`
	}
}

type okexKlineResponse struct {
	okexErrorResponse
	Data [][]interface{}
}

func (resp *okexTickerResponse) getCommonResponse() okexErrorResponse {
	return resp.okexErrorResponse
}

func (resp *okexTickerResponse) getInternalData() interface{} {
	return resp
}

func (resp *okexKlineResponse) getCommonResponse() okexErrorResponse {
	return resp.okexErrorResponse
}

func (resp *okexKlineResponse) getInternalData() interface{} {
	return &resp.Data
}

// Any way to hold the common response, instead of adding an interface here?
type okexCommonResponseProvider interface {
	getCommonResponse() okexErrorResponse
	getInternalData() interface{}
}

func (client *okexClient) GetName() string {
	return "OKEx"
}

func (client *okexClient) decodeResponse(respBytes []byte, respJSON okexCommonResponseProvider) error {
	// What a messy
	respBody := strings.TrimSpace(string(respBytes))
	if respBody[0] == '[' {
		return json.Unmarshal(respBytes, respJSON.getInternalData())
	}

	if err := json.Unmarshal(respBytes, &respJSON); err != nil {
		return err
	}

	// All I need is to get the common part, I don't like this
	commonResponse := respJSON.getCommonResponse()
	if commonResponse.ErrorCode != 0 {
		return fmt.Errorf("error_code: %v", commonResponse.ErrorCode)
	}
	return nil
}

func (client *okexClient) GetKlinePrice(symbol, period string, size int) (float64, error) {
	symbol = strings.ToLower(symbol)
	respByte, err := http.Get(okexBaseApi+"/kline.do", map[string]string{
		"symbol": symbol,
		"type":   period,
		"size":   strconv.Itoa(size),
	})
	if err != nil {
		return 0, err
	}

	var respJSON okexKlineResponse
	err = client.decodeResponse(respByte, &respJSON)
	if err != nil {
		return 0, err
	}
	logrus.Debugf("%s - Kline for %s*%v uses price at %s", client.GetName(), period, size,
		time.Unix(int64(respJSON.Data[0][0].(float64))/1000, 0))
	return strconv.ParseFloat(respJSON.Data[0][1].(string), 64)
}

func (client *okexClient) GetSymbolPrice(symbol string) (*model.SymbolPrice, error) {
	respByte, err := http.Get(okexBaseApi+"/ticker.do", map[string]string{"symbol": strings.ToLower(symbol)})
	if err != nil {
		return nil, err
	}

	var respJSON okexTickerResponse
	err = client.decodeResponse(respByte, &respJSON)
	if err != nil {
		return nil, err
	}

	var percentChange1h, percentChange24h = math.MaxFloat64, math.MaxFloat64
	price1hAgo, err := client.GetKlinePrice(symbol, "1min", 60)
	if err != nil {
		logrus.Warnf("%s - Failed to get price 1 hour ago, error: %v\n", client.GetName(), err)
	} else if price1hAgo != 0 {
		percentChange1h = (respJSON.Ticker.Last - price1hAgo) / price1hAgo * 100
	}

	time.Sleep(time.Second)                                       // Limit 1 req/sec for Kline
	price24hAgo, err := client.GetKlinePrice(symbol, "3min", 492) // Why not 480?
	if err != nil {
		logrus.Warnf("%s - Failed to get price 24 hours ago, error: %v\n", client.GetName(), err)
	} else if price24hAgo != 0 {
		percentChange24h = (respJSON.Ticker.Last - price24hAgo) / price24hAgo * 100
	}

	return &model.SymbolPrice{
		Symbol:           symbol,
		Price:            strconv.FormatFloat(respJSON.Ticker.Last, 'f', -1, 64),
		UpdateAt:         time.Unix(respJSON.Date, 0),
		Source:           client.GetName(),
		PercentChange1h:  percentChange1h,
		PercentChange24h: percentChange24h,
	}, nil
}

func init() {
	model.Register(new(okexClient))
}
