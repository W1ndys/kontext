package llm

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// DefaultMaxRetries 是默认最大重试次数。
const DefaultMaxRetries = 3

// ChatWithRetry 带自动重试的 Chat 调用，遇到可重试错误时自动重试。
func ChatWithRetry(client Client, req *ChatRequest, maxRetries int, onRetry func(attempt int, err error, backoff time.Duration)) (*ChatResponse, error) {
	if maxRetries <= 0 {
		maxRetries = DefaultMaxRetries
	}

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		resp, err := client.Chat(req)
		if err == nil {
			return resp, nil
		}

		// 判断是否可重试的错误
		if !isRetryableError(err) {
			return nil, err
		}

		lastErr = err
		backoff := time.Duration(math.Pow(2, float64(i))) * time.Second
		if backoff > 30*time.Second {
			backoff = 30 * time.Second // 最大退避时间 30 秒
		}

		if onRetry != nil {
			onRetry(i+1, err, backoff)
		}

		time.Sleep(backoff)
	}

	return nil, fmt.Errorf("重试 %d 次后仍然失败: %w", maxRetries, lastErr)
}

// ChatStructuredWithRetry 带自动重试的 ChatStructured 调用。
func ChatStructuredWithRetry(client Client, req *ChatRequest, schemaName string, out any, maxRetries int, onRetry func(attempt int, err error, backoff time.Duration)) (*ChatResponse, error) {
	if maxRetries <= 0 {
		maxRetries = DefaultMaxRetries
	}

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		resp, err := client.ChatStructured(req, schemaName, out)
		if err == nil {
			return resp, nil
		}

		// 判断是否可重试的错误
		if !isRetryableError(err) {
			return nil, err
		}

		lastErr = err
		backoff := time.Duration(math.Pow(2, float64(i))) * time.Second
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}

		if onRetry != nil {
			onRetry(i+1, err, backoff)
		}

		time.Sleep(backoff)
	}

	return nil, fmt.Errorf("重试 %d 次后仍然失败: %w", maxRetries, lastErr)
}

// ChatStreamWithRetry 带自动重试的 ChatStream 调用。
func ChatStreamWithRetry(client Client, req *ChatRequest, onChunk func(string) error, maxRetries int, onRetry func(attempt int, err error, backoff time.Duration)) (*ChatResponse, error) {
	if maxRetries <= 0 {
		maxRetries = DefaultMaxRetries
	}

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		resp, err := client.ChatStream(req, onChunk)
		if err == nil {
			return resp, nil
		}

		if !isRetryableError(err) {
			return nil, err
		}

		lastErr = err
		backoff := time.Duration(math.Pow(2, float64(i))) * time.Second
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}

		if onRetry != nil {
			onRetry(i+1, err, backoff)
		}

		time.Sleep(backoff)
	}

	return nil, fmt.Errorf("重试 %d 次后仍然失败: %w", maxRetries, lastErr)
}

// isRetryableError 判断错误是否可以重试。
// 网络错误、超时、服务端临时错误（5xx）都可以重试。
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	// 网络相关错误
	networkErrors := []string{
		"eof",
		"unexpected eof",
		"connection reset",
		"connection refused",
		"connection timed out",
		"timeout",
		"context deadline exceeded",
		"no such host",
		"network is unreachable",
		"i/o timeout",
		"temporary failure",
		"tls handshake timeout",
	}

	for _, netErr := range networkErrors {
		if strings.Contains(errStr, netErr) {
			return true
		}
	}

	// HTTP 5xx 错误（服务端临时错误）
	serverErrors := []string{
		"500", "internal server error",
		"502", "bad gateway",
		"503", "service unavailable",
		"504", "gateway timeout",
		"520", "521", "522", "523", "524", // Cloudflare errors
	}

	for _, srvErr := range serverErrors {
		if strings.Contains(errStr, srvErr) {
			return true
		}
	}

	// Rate limit 错误
	rateLimitErrors := []string{
		"rate limit",
		"too many requests",
		"429",
	}

	for _, rlErr := range rateLimitErrors {
		if strings.Contains(errStr, rlErr) {
			return true
		}
	}

	return false
}
