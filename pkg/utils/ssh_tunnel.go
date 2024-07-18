package utils

import (
	"fmt"
	"golang.org/x/crypto/ssh"
	"io"
	"net"
)

func CreateSSHTunnel(localAddr, sshAddr, remoteAddr string, config *ssh.ClientConfig) error {
	sshClient, err := ssh.Dial("tcp", sshAddr, config)
	if err != nil {
		return fmt.Errorf("SSH dial error: %v", err)
	}

	listener, err := net.Listen("tcp", localAddr)
	if err != nil {
		return fmt.Errorf("local listen error: %v", err)
	}

	for {
		localConn, err := listener.Accept()
		if err != nil {
			return fmt.Errorf("local accept error: %v", err)
		}

		go func() {
			remoteConn, err := sshClient.Dial("tcp", remoteAddr)
			if err != nil {
				fmt.Printf("Remote dial error: %v\n", err)
				return
			}

			go copyConn(localConn, remoteConn)
			go copyConn(remoteConn, localConn)
		}()
	}
}

func copyConn(writer, reader net.Conn) {
	_, err := io.Copy(writer, reader)
	if err != nil {
		fmt.Printf("io.Copy error: %v\n", err)
	}
}
