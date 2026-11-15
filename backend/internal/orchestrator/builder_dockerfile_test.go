package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerfileBuilder(t *testing.T) {
	b := NewDockerfileBuilder("", nil)

	t.Run("Name returns dockerfile", func(t *testing.T) {
		if b.Name() != "dockerfile" {
			t.Errorf("expected dockerfile, got %q", b.Name())
		}
	})

	t.Run("PlanAnalyze parses EXPOSE, CMD, FROM and ENV", func(t *testing.T) {
		tmpDir := t.TempDir()
		dfContent := `
FROM python:3.11-slim
ENV APP_ENV=production \
    DEBUG=false
WORKDIR /app
COPY requirements.txt .
RUN pip install -r requirements.txt
COPY . .
EXPOSE 5000
CMD ["gunicorn", "--bind", "0.0.0.0:5000", "back:app"]
`
		if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(dfContent), 0o644); err != nil {
			t.Fatal(err)
		}

		plan, err := b.PlanAnalyze(context.Background(), tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if plan.Builder != "dockerfile" {
			t.Errorf("expected builder dockerfile, got %q", plan.Builder)
		}
		if plan.DetectedLanguage != "python" {
			t.Errorf("expected language python, got %q", plan.DetectedLanguage)
		}
		if plan.InternalPort != 5000 {
			t.Errorf("expected port 5000, got %d", plan.InternalPort)
		}
		if plan.StartCmd != "gunicorn --bind 0.0.0.0:5000 back:app" {
			t.Errorf("expected clean start command, got %q", plan.StartCmd)
		}
		if plan.Variables["APP_ENV"] != "production" {
			t.Errorf("expected APP_ENV=production, got %q", plan.Variables["APP_ENV"])
		}
	})

	t.Run("PlanAnalyze handles shell CMD and ENTRYPOINT combinations", func(t *testing.T) {
		tmpDir := t.TempDir()
		dfContent := `
FROM node:20-alpine
ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["npm", "start"]
EXPOSE 3000
`
		if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(dfContent), 0o644); err != nil {
			t.Fatal(err)
		}

		plan, err := b.PlanAnalyze(context.Background(), tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if plan.DetectedLanguage != "node" {
			t.Errorf("expected language node, got %q", plan.DetectedLanguage)
		}
		if plan.InternalPort != 3000 {
			t.Errorf("expected port 3000, got %d", plan.InternalPort)
		}
		if plan.StartCmd != "docker-entrypoint.sh npm start" {
			t.Errorf("expected composite start command, got %q", plan.StartCmd)
		}
	})

	t.Run("PlanAnalyze defaults to port 8080 when EXPOSE missing", func(t *testing.T) {
		tmpDir := t.TempDir()
		dfContent := `
FROM golang:1.24
CMD ["./server"]
`
		if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(dfContent), 0o644); err != nil {
			t.Fatal(err)
		}

		plan, err := b.PlanAnalyze(context.Background(), tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if plan.InternalPort != 8080 {
			t.Errorf("expected default port 8080, got %d", plan.InternalPort)
		}
		if plan.DetectedLanguage != "go" {
			t.Errorf("expected language go, got %q", plan.DetectedLanguage)
		}
	})

	t.Run("PlanAnalyze returns error when no Dockerfile found", func(t *testing.T) {
		tmpDir := t.TempDir()
		_, err := b.PlanAnalyze(context.Background(), tmpDir)
		if err == nil {
			t.Fatal("expected error for missing Dockerfile, got nil")
		}
		if !strings.Contains(err.Error(), "no Dockerfile found") {
			t.Errorf("expected 'no Dockerfile found' in error, got: %v", err)
		}
	})

	t.Run("cleanCommandString formats arrays and raw strings", func(t *testing.T) {
		cases := []struct {
			input    string
			expected string
		}{
			{`["python", "main.py"]`, "python main.py"},
			{`["npm", "run", "start:prod"]`, "npm run start:prod"},
			{`python -m uvicorn app:app`, "python -m uvicorn app:app"},
			{`['node', 'index.js']`, "node index.js"},
		}
		for _, tc := range cases {
			if got := cleanCommandString(tc.input); got != tc.expected {
				t.Errorf("cleanCommandString(%q) = %q; want %q", tc.input, got, tc.expected)
			}
		}
	})
}

