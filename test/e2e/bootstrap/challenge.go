package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type httpError struct {
	method string
	path   string
	status int
	body   []byte
}

func (e *httpError) Error() string {
	return fmt.Sprintf("%s %s: HTTP %d: %s", e.method, e.path, e.status, strings.TrimSpace(string(e.body)))
}

// callWithChallenge sends a browser-style mutation and, when the server answers
// 428 with a proof-of-work challenge, solves it the way the console's Cap widget
// does and retries once with the proof and the same idempotency key.
func (s *session) callWithChallenge(method, path string, body any, out any) error {
	headers := idempotency()
	err := s.call(method, path, body, headers, out)
	var he *httpError
	if !errors.As(err, &he) || he.status != http.StatusPreconditionRequired {
		return err
	}
	var problem struct {
		Details struct {
			Challenge struct {
				Action string `json:"action"`
			} `json:"challenge"`
		} `json:"details"`
	}
	if err := json.Unmarshal(he.body, &problem); err != nil {
		return err
	}
	proof, err := s.solveChallenge(problem.Details.Challenge.Action, headers["Idempotency-Key"])
	if err != nil {
		return err
	}
	headers["X-CCH-Challenge-Proof"] = proof
	return s.call(method, path, body, headers, out)
}

func (s *session) solveChallenge(action, idempotencyKey string) (string, error) {
	var created struct {
		ChallengeID string `json:"challengeId"`
		Plan        struct {
			AllOf []struct {
				Type   string `json:"type"`
				TaskID string `json:"taskId"`
			} `json:"allOf"`
		} `json:"plan"`
	}
	req := map[string]any{"action": action, "idempotencyKey": idempotencyKey}
	if err := s.call(http.MethodPost, "/api/security/challenges", req, nil, &created); err != nil {
		return "", err
	}
	var evidence []map[string]any
	for _, task := range created.Plan.AllOf {
		if task.Type != "pow" {
			return "", fmt.Errorf("challenge task type %q needs a browser", task.Type)
		}
		var cap struct {
			Token string `json:"token"`
			C     int    `json:"c"`
			S     int    `json:"s"`
			D     int    `json:"d"`
		}
		if err := s.call(http.MethodPost, "/api/security/cap/challenge", map[string]any{"challengeId": created.ChallengeID}, nil, &cap); err != nil {
			return "", err
		}
		solutions, err := solveCap(cap.Token, cap.C, cap.S, cap.D)
		if err != nil {
			return "", err
		}
		var redeemed struct {
			Token string `json:"token"`
		}
		if err := s.call(http.MethodPost, "/api/security/cap/redeem", map[string]any{"token": cap.Token, "solutions": solutions}, nil, &redeemed); err != nil {
			return "", err
		}
		evidence = append(evidence, map[string]any{"taskId": task.TaskID, "type": "pow", "token": redeemed.Token})
	}
	var verified struct {
		Proof string `json:"proof"`
	}
	if err := s.call(http.MethodPost, "/api/security/challenges/"+created.ChallengeID+"/verify", map[string]any{"evidence": evidence}, nil, &verified); err != nil {
		return "", err
	}
	return verified.Proof, nil
}

// solveCap finds, for each of the c puzzles, the smallest nonce whose
// hex(SHA-256(salt+nonce)) starts with the derived target (the cap.js scheme).
func solveCap(token string, c, saltLen, targetLen int) ([]int64, error) {
	out := make([]int64, c)
	for i := 1; i <= c; i++ {
		salt := capPRNG(token+strconv.Itoa(i), saltLen)
		target := capPRNG(token+strconv.Itoa(i)+"d", targetLen)
		found := false
		for nonce := range int64(50_000_000) {
			sum := sha256.Sum256([]byte(salt + strconv.FormatInt(nonce, 10)))
			if strings.HasPrefix(hex.EncodeToString(sum[:]), target) {
				out[i-1] = nonce
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("cap puzzle %d has no solution in range", i)
		}
	}
	return out, nil
}

func capPRNG(seed string, length int) string {
	var state uint32 = 2166136261
	for i := 0; i < len(seed); i++ {
		state ^= uint32(seed[i])
		state += (state << 1) + (state << 4) + (state << 7) + (state << 8) + (state << 24)
	}
	const digits = "0123456789abcdef"
	var b strings.Builder
	for b.Len() < length {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		var buf [8]byte
		v := state
		for i := 7; i >= 0; i-- {
			buf[i] = digits[v&0xf]
			v >>= 4
		}
		b.Write(buf[:])
	}
	return b.String()[:length]
}
