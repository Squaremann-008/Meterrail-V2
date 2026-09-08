// Package envio queries the Envio HyperIndex GraphQL API. HyperIndex serves
// indexed onchain data over Hasura, so everything here is a POST of a query
// document plus variables.
package envio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/meterrail/api/internal/config"
)

type Client struct {
	http   *http.Client
	url    string
	secret string
	logger *slog.Logger
}

func New(cfg config.Envio, logger *slog.Logger) *Client {
	return &Client{
		http:   &http.Client{Timeout: cfg.Timeout},
		url:    cfg.GraphQLURL,
		secret: cfg.AdminSecret,
		logger: logger,
	}
}

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphQLError struct {
	Message string `json:"message"`
	Path    []any  `json:"path,omitempty"`
}

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors,omitempty"`
}

// Query executes a GraphQL document and decodes `data` into out.
func (c *Client) Query(ctx context.Context, query string, variables map[string]any, out any) error {
	body, err := json.Marshal(graphQLRequest{Query: query, Variables: variables})
	if err != nil {
		return fmt.Errorf("envio: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("envio: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.secret != "" {
		req.Header.Set("x-hasura-admin-secret", c.secret)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("envio: request %s: %w", c.url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("envio: unexpected status %s", resp.Status)
	}

	var decoded graphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return fmt.Errorf("envio: decode response: %w", err)
	}
	if len(decoded.Errors) > 0 {
		messages := make([]string, len(decoded.Errors))
		for i, e := range decoded.Errors {
			messages[i] = e.Message
		}
		return fmt.Errorf("envio: graphql errors: %s", strings.Join(messages, "; "))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(decoded.Data, out); err != nil {
		return fmt.Errorf("envio: decode data: %w", err)
	}
	return nil
}

// Health checks that the indexer answers a trivial query.
func (c *Client) Health(ctx context.Context) error {
	return c.Query(ctx, `query Health { __typename }`, nil, nil)
}

// Transfer is one indexed event as HyperIndex stores it. Field names match the
// entity declared in indexer/schema.graphql.
type Transfer struct {
	ID             string `json:"id"`
	ChainID        int64  `json:"chainId"`
	BlockNumber    int64  `json:"blockNumber"`
	BlockTimestamp int64  `json:"blockTimestamp"`
	TxHash         string `json:"transactionHash"`
	LogIndex       int    `json:"logIndex"`
	Contract       string `json:"contractAddress"`
	From           string `json:"from"`
	To             string `json:"to"`
	Value          string `json:"value"`
}

// BlockTime converts the indexer's unix seconds into a time.Time.
func (t Transfer) BlockTime() time.Time {
	return time.Unix(t.BlockTimestamp, 0).UTC()
}

const transfersQuery = `
query Transfers($chainId: Int!, $afterBlock: numeric!, $limit: Int!) {
  Transfer(
    where: { chainId: { _eq: $chainId }, blockNumber: { _gt: $afterBlock } }
    order_by: [{ blockNumber: asc }, { logIndex: asc }]
    limit: $limit
  ) {
    id
    chainId
    blockNumber
    blockTimestamp
    transactionHash
    logIndex
    contractAddress
    from
    to
    value
  }
}`

// TransfersAfter pages events newer than a block height. The sync job calls
// this in a loop, advancing the checkpoint until a short page comes back.
func (c *Client) TransfersAfter(ctx context.Context, chainID, afterBlock int64, limit int) ([]Transfer, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	var payload struct {
		Transfer []Transfer `json:"Transfer"`
	}
	err := c.Query(ctx, transfersQuery, map[string]any{
		"chainId":    chainID,
		"afterBlock": afterBlock,
		"limit":      limit,
	}, &payload)
	if err != nil {
		return nil, err
	}
	return payload.Transfer, nil
}

// SyncStatus reports how far each chain's indexer has progressed, which the
// health endpoint surfaces as indexer lag.
type SyncStatus struct {
	ChainID     int64 `json:"chain_id"`
	BlockHeight int64 `json:"block_height"`
	IsSynced    bool  `json:"is_hyper_sync"`
}

const syncQuery = `
query SyncStatus {
  chain_metadata {
    chain_id
    block_height
    is_hyper_sync
  }
}`

func (c *Client) SyncStatus(ctx context.Context) ([]SyncStatus, error) {
	var payload struct {
		ChainMetadata []SyncStatus `json:"chain_metadata"`
	}
	if err := c.Query(ctx, syncQuery, nil, &payload); err != nil {
		return nil, err
	}
	return payload.ChainMetadata, nil
}
