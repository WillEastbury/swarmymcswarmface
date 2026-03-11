package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	externalscaler "github.com/kedacore/keda/v2/pkg/scalers/externalscaler"
	"google.golang.org/grpc"
)

type server struct {
	externalscaler.UnimplementedExternalScalerServer
	metricName   string
	targetValue  int64
	pendingCount atomic.Int64
}

type githubIssueEvent struct {
	Action string `json:"action"`
	Issue  struct {
		ID     int64  `json:"id"`
		Number int64  `json:"number"`
		State  string `json:"state"`
	} `json:"issue"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func main() {
	grpcAddr := getEnv("GRPC_ADDR", ":9090")
	httpAddr := getEnv("HTTP_ADDR", ":8080")
	metricName := getEnv("KEDA_METRIC_NAME", "github_issue_events_pending")
	targetValue := mustInt64(getEnv("KEDA_TARGET_VALUE", "1"))
	webhookSecret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	filterRepo := os.Getenv("GITHUB_FILTER_REPO") // optional: owner/repo

	s := &server{
		metricName:  metricName,
		targetValue: targetValue,
	}

	go startWebhookServer(httpAddr, webhookSecret, filterRepo, s)
	startGrpcServer(grpcAddr, s)
}

func startWebhookServer(addr, webhookSecret, filterRepo string, s *server) {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/webhook/github", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		eventType := r.Header.Get("X-GitHub-Event")
		if eventType != "issues" {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		body, err := readBody(r)
		if err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}

		if webhookSecret != "" {
			if !verifyGithubSignature(webhookSecret, r.Header.Get("X-Hub-Signature-256"), body) {
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}
		}

		var payload githubIssueEvent
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		if filterRepo != "" && payload.Repository.FullName != filterRepo {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		switch payload.Action {
		case "opened", "reopened":
			s.pendingCount.Add(1)
		case "closed":
			current := s.pendingCount.Load()
			if current > 0 {
				s.pendingCount.Add(-1)
			}
		}

		log.Printf("issue event action=%s repo=%s issue=%d pending=%d",
			payload.Action, payload.Repository.FullName, payload.Issue.Number, s.pendingCount.Load())

		w.WriteHeader(http.StatusOK)
	})

	log.Printf("webhook server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("webhook server failed: %v", err)
	}
}

func startGrpcServer(addr string, s *server) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to bind grpc: %v", err)
	}

	grpcServer := grpc.NewServer()
	externalscaler.RegisterExternalScalerServer(grpcServer, s)
	log.Printf("keda external scaler grpc listening on %s", addr)

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("grpc server failed: %v", err)
	}
}

func (s *server) IsActive(context.Context, *externalscaler.ScaledObjectRef) (*externalscaler.IsActiveResponse, error) {
	return &externalscaler.IsActiveResponse{Result: s.pendingCount.Load() > 0}, nil
}

func (s *server) StreamIsActive(_ *externalscaler.ScaledObjectRef, stream externalscaler.ExternalScaler_StreamIsActiveServer) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		active := s.pendingCount.Load() > 0
		if err := stream.Send(&externalscaler.IsActiveResponse{Result: active}); err != nil {
			return err
		}
	}
	return nil
}

func (s *server) GetMetricSpec(context.Context, *externalscaler.ScaledObjectRef) (*externalscaler.GetMetricSpecResponse, error) {
	return &externalscaler.GetMetricSpecResponse{
		MetricSpecs: []*externalscaler.MetricSpec{
			{
				MetricName: s.metricName,
				TargetSize: s.targetValue,
			},
		},
	}, nil
}

func (s *server) GetMetrics(context.Context, *externalscaler.GetMetricsRequest) (*externalscaler.GetMetricsResponse, error) {
	return &externalscaler.GetMetricsResponse{
		MetricValues: []*externalscaler.MetricValue{
			{
				MetricName:  s.metricName,
				MetricValue: s.pendingCount.Load(),
			},
		},
	}, nil
}

func verifyGithubSignature(secret, signatureHeader string, body []byte) bool {
	if !strings.HasPrefix(signatureHeader, "sha256=") {
		return false
	}
	got := strings.TrimPrefix(signatureHeader, "sha256=")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(got), []byte(expected))
}

func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	const maxBody = 2 << 20 // 2MiB
	limited := io.LimitReader(r.Body, maxBody)
	body, err := io.ReadAll(limited)
	if err != nil && !errors.Is(err, context.Canceled) {
		return nil, err
	}
	return body, nil
}

func getEnv(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func mustInt64(v string) int64 {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		panic(fmt.Sprintf("invalid int %q: %v", v, err))
	}
	return n
}
