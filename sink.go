package scp

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

func CopyFileFromRemote(client *ssh.Client, remoteFilename, localFilename string, updatesPermission, setTime bool) error {
	remoteFilename = filepath.Clean(remoteFilename)
	localFilename = filepath.Clean(localFilename)

	s := NewSink(client, remoteFilename, false, "", false, updatesPermission)

	copier := func(s *Sink) error {
		var timeHeader TimeMsgHeader
		if setTime {
			h, err := s.ReadHeaderOrReply()
			if err != nil {
				return fmt.Errorf("failed to read scp message header: err=%s", err)
			}
			var ok bool
			timeHeader, ok = h.(TimeMsgHeader)
			if !ok {
				return fmt.Errorf("expected time message header, got %+v", h)
			}
		}

		h, err := s.ReadHeaderOrReply()
		if err != nil {
			return fmt.Errorf("failed to read scp message header: err=%s", err)
		}
		fileHeader, ok := h.(FileMsgHeader)
		if !ok {
			return fmt.Errorf("expected file message header, got %+v", h)
		}

		file, err := os.OpenFile(localFilename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, fileHeader.Mode)
		if err != nil {
			return fmt.Errorf("failed to open destination file: err=%s", err)
		}
		defer file.Close()

		err = s.CopyFileBodyTo(fileHeader, file)
		if err != nil {
			return fmt.Errorf("failed to copy file: err=%s", err)
		}

		if updatesPermission {
			err := os.Chmod(localFilename, fileHeader.Mode)
			if err != nil {
				return fmt.Errorf("failed to change file mode: err=%s", err)
			}
		}

		if setTime {
			err := os.Chtimes(localFilename, timeHeader.Atime, timeHeader.Mtime)
			if err != nil {
				return fmt.Errorf("failed to change file time: err=%s", err)
			}
		}

		return nil
	}
	return s.CopyFromRemote(copier)
}

func CopyRecursivelyFromRemote(client *ssh.Client, srcDir, destDir string, updatesPermission, setTime bool, walkFn filepath.WalkFunc) error {
	srcDir = filepath.Clean(srcDir)
	destDir = filepath.Clean(destDir)

	s := NewSink(client, destDir, true, "", true, updatesPermission)

	copier := func(s *Sink) error {

		return nil
	}
	return s.CopyFromRemote(copier)
}

type Sink struct {
	client            *ssh.Client
	session           *ssh.Session
	remoteSrcPath     string
	remoteSrcIsDir    bool
	scpPath           string
	recursive         bool
	updatesPermission bool
	*sinkProtocol
}

func NewSink(client *ssh.Client, remoteSrcPath string, remoteSrcIsDir bool, scpPath string, recursive, updatesPermission bool) *Sink {
	return &Sink{
		client:            client,
		remoteSrcPath:     remoteSrcPath,
		remoteSrcIsDir:    remoteSrcIsDir,
		scpPath:           scpPath,
		recursive:         recursive,
		updatesPermission: updatesPermission,
	}
}

func (s *Sink) CopyFromRemote(copier func(src *Sink) error) error {
	var err error
	s.session, err = s.client.NewSession()
	if err != nil {
		return err
	}
	defer s.session.Close()

	stdout, err := s.session.StdoutPipe()
	if err != nil {
		return err
	}

	stdin, err := s.session.StdinPipe()
	if err != nil {
		return err
	}

	err = func() error {
		defer stdin.Close()

		if s.scpPath == "" {
			s.scpPath = "scp"
		}

		opt := []byte("-f")
		if s.updatesPermission {
			opt = append(opt, 'p')
		}
		if s.recursive {
			opt = append(opt, 'r')
		}
		if s.remoteSrcIsDir {
			opt = append(opt, 'd')
		}

		cmd := s.scpPath + " " + string(opt) + " " + EscapeShellArg(s.remoteSrcPath)
		err = s.session.Start(cmd)
		if err != nil {
			return err
		}

		s.sinkProtocol, err = newSinkProtocol(stdin, stdout)
		if err != nil {
			return err
		}
		return copier(s)
	}()
	if err != nil {
		return err
	}

	return s.session.Wait()
}
