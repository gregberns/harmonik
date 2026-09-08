package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

// dialKernel opens a plain, unauthenticated connection to a KernelService
// endpoint. It is used both by the client verbs below and by tests acting
// as an SDK-less caller. A gRPC client connects lazily, so a bad address
// surfaces on the first real call, not here.
func dialKernel(addr string) (kernelv1.KernelServiceClient, func() error, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, fmt.Errorf("harmonikd: dial %q: %w", addr, err)
	}
	return kernelv1.NewKernelServiceClient(conn), conn.Close, nil
}

// clientPublish sends one payload to channel and reports the message id the
// kernel assigned it.
func clientPublish(ctx context.Context, kernelAddr, channel string, payload []byte) (string, error) {
	client, closeFn, err := dialKernel(kernelAddr)
	if err != nil {
		return "", err
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			slog.ErrorContext(ctx, "harmonikd: close kernel connection", "error", closeErr)
		}
	}()

	resp, err := client.Publish(ctx, &kernelv1.PublishRequest{Channel: channel, Payload: payload})
	if err != nil {
		return "", fmt.Errorf("harmonikd: publish: %w", err)
	}
	return resp.GetMessageId(), nil
}

// clientJournalRead returns every record a journal currently holds, each
// hex-encoded so a binary payload survives the round trip to a terminal.
func clientJournalRead(ctx context.Context, kernelAddr, journal string) ([]string, error) {
	client, closeFn, err := dialKernel(kernelAddr)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			slog.ErrorContext(ctx, "harmonikd: close kernel connection", "error", closeErr)
		}
	}()

	stream, err := client.JournalRead(ctx, &kernelv1.JournalReadRequest{Journal: journal})
	if err != nil {
		return nil, fmt.Errorf("harmonikd: journal read: %w", err)
	}

	var lines []string
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("harmonikd: journal read: %w", err)
		}
		for _, rec := range resp.GetRecords() {
			lines = append(lines, hex.EncodeToString(rec.GetRecord()))
		}
	}
	return lines, nil
}
