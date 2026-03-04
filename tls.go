package task

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"

	"github.com/benaskins/axon"
)

type contextKey string

// ClientCNKey is the context key used to store the client certificate CN.
const ClientCNKey contextKey = "clientCN"

// RequireClientCert returns middleware that rejects requests without a verified client certificate.
// It extracts the client certificate CN and adds it to the request context for audit logging.
func RequireClientCert(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			axon.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "client certificate required"})
			return
		}
		cn := r.TLS.PeerCertificates[0].Subject.CommonName
		ctx := context.WithValue(r.Context(), ClientCNKey, cn)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// LoadTLSConfig loads a CA certificate for client verification and returns a TLS config.
func LoadTLSConfig(caCertPath string) (*tls.Config, error) {
	caCert, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, err
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}
	return &tls.Config{
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  caPool,
		MinVersion: tls.VersionTLS12,
	}, nil
}
