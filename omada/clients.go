package omada

import (
	"fmt"
	"net/url"

	"go.uber.org/zap"
)

const maxPages = 10 // Safety limit to prevent infinite loops

// ClientsResult models the API response for client listings
type ClientsResult struct {
	ErrorCode int64 `json:"errorCode"`
	Msg       string
	Result    struct {
		CurrentPage int64              `json:"currentPage"`
		CurrentSize int64              `json:"currentSize"`
		TotalRows   int64              `json:"totalRows"`
		Data        []*ConnectedClient `json:"data"`
	} `json:"result"`
}

// ConnectedClient represents a connected client object
type ConnectedClient struct {
	Active         bool   `json:"active"`
	Activity       int64  `json:"activity"`
	ApMAC          string `json:"apMac"`
	ApName         string `json:"apName"`
	AuthStatus     int64  `json:"authStatus"`
	Channel        int64  `json:"channel"`
	ConnectDevType string `json:"connectDevType"`
	ConnectType    int64  `json:"connectType"`
	DeviceType     string `json:"deviceType"`
	DownPacket     int64  `json:"downPacket"`
	Guest          bool   `json:"guest"`
	HostName       string `json:"hostName"`
	IP             string `json:"ip"`
	LastSeen       int64  `json:"lastSeen"`
	MAC            string `json:"mac"`
	Manager        bool   `json:"manager"`
	Name           string `json:"name"`
	PowerSave      bool   `json:"powerSave"`
	RadioID        int64  `json:"radioId"`
	RSSI           int64  `json:"rssi"`
	RxRate         int64  `json:"rxRate"`
	SignalLevel    int64  `json:"signalLevel"`
	SignalRank     int64  `json:"signalRank"`
	SSID           string `json:"ssid"`
	TrafficDown    int64  `json:"trafficDown"`
	TrafficUp      int64  `json:"trafficUp"`
	TxRate         int64  `json:"txRate"`
	UpPacket       int64  `json:"upPacket"`
	Uptime         int64  `json:"uptime"`
	WifiMode       int64  `json:"wifiMode"`
	Wireless       bool   `json:"wireless"`
}

// dedupeClients removes duplicate entries by MAC address
func dedupeClients(in []*ConnectedClient) []*ConnectedClient {
	seen := make(map[string]struct{}, len(in))
	out := make([]*ConnectedClient, 0, len(in))

	for _, item := range in {
		if _, ok := seen[item.MAC]; !ok {
			seen[item.MAC] = struct{}{}
			out = append(out, item)
		}
	}

	return out
}

// ConnectedClients returns active wireless clients for a given site
func (c *Client) ConnectedClients(site string) ([]*ConnectedClient, error) {
	var clients []*ConnectedClient
	currentPage := 1

	for ; currentPage <= maxPages; currentPage++ {
		var r ClientsResult

		get := func() error {
			// NOTE: Removed `token` from query string, passed in header instead
			path := fmt.Sprintf(
				"/api/v2/sites/%s/clients?currentPage=%d&currentPageSize=100&filters.active=true",
				url.QueryEscape(site),
				currentPage,
			)
			err := c.getJSON(path, &r)
			if err != nil {
				return err
			}
			if r.ErrorCode == -1200 {
				return errTokenExpired
			}
			if r.ErrorCode != 0 {
				return fmt.Errorf("%v: %s", r.ErrorCode, r.Msg)
			}
			return nil
		}

		if err := c.retryOnce(get); err != nil {
			return nil, err
		}

		clients = append(clients, r.Result.Data...)

		if int64(len(clients)) >= r.Result.TotalRows || len(r.Result.Data) == 0 {
			break
		}
	}

	if currentPage > maxPages {
		c.logger.Warn("stopped fetching clients after too many pages",
			zap.Int("pages", maxPages),
			zap.Int("clients", len(clients)),
		)
	}

	// Deduplicate clients before returning
	return dedupeClients(clients), nil
}