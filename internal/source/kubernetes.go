package source

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os/exec"
)

type K8sSource struct {
	pvc      string
	snapshot bool
}

func NewK8sSource(pvc string, snapshot bool) *K8sSource {
	return &K8sSource{pvc: pvc, snapshot: snapshot}
}

func (s *K8sSource) Type() string { return "kubernetes" }

func (s *K8sSource) Name() string { return "k8s-pvc:" + s.pvc }

func (s *K8sSource) Capture(ctx context.Context) (io.ReadCloser, error) {
	if s.snapshot {
		return nil, fmt.Errorf("k8s snapshot mode requires client-go (use snapshot: false for exec-based backup)")
	}

	podName := "backupd-temp-" + randomSuffix()

	create := exec.CommandContext(ctx, "kubectl", "run", podName,
		"--image=busybox:latest",
		"--restart=Never",
		"--overrides=`{\"spec\":{\"volumes\":[{\"name\":\"data\",\"persistentVolumeClaim\":{\"claimName\":\""+s.pvc+"\"}}],\"containers\":[{\"name\":\"backupd-temp\",\"image\":\"busybox:latest\",\"command\":[\"sleep\",\"3600\"],\"volumeMounts\":[{\"name\":\"data\",\"mountPath\":\"/data\",\"readOnly\":true}]}]}}`")
	create.Stderr = new(bytes.Buffer)
	if err := create.Run(); err != nil {
		return nil, fmt.Errorf("creating temp pod: %w", err)
	}

	pr, pw := io.Pipe()

	go func() {
		defer func() {
			delCmd := exec.CommandContext(context.Background(), "kubectl", "delete", "pod", podName, "--ignore-not-found=true", "--wait=false")
			if err := delCmd.Run(); err != nil {
				log.Printf("error deleting temp pod %s: %v", podName, err)
			}
		}()

		tarCmd := exec.CommandContext(ctx, "kubectl", "exec", podName, "--",
			"tar", "cf", "-", "-C", "/data", ".")
		tarCmd.Stdout = pw
		tarCmd.Stderr = new(bytes.Buffer)

		err := tarCmd.Run()
		if err != nil {
			pw.CloseWithError(fmt.Errorf("tar exec: %w", err))
			return
		}

		pw.Close()
	}()

	return pr, nil
}

func randomSuffix() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "abcdefgh"
	}
	return hex.EncodeToString(b)
}
