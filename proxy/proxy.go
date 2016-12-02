/*
 Copyright 2016 Crunchy Data Solutions, Inc.
 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

      http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package proxy

import (
	"github.com/crunchydata/crunchy-proxy/config"
	"github.com/golang/glog"
	"net"
)

func ListenAndServe(config *config.Config) {
	glog.Infoln("[proxy] ListenAndServe config=" + config.Name)
	glog.Infoln("[proxy] ListenAndServe listening on ipaddr=" + config.IPAddr)

	tcpAddr, err := net.ResolveTCPAddr("tcp", config.IPAddr)
	checkError(err)

	var listener net.Listener

	listener, err = net.ListenTCP("tcp", tcpAddr)
	checkError(err)

	handleListener(config, listener)
}

func handleListener(config *config.Config, listener net.Listener) {
	for {
		conn, err := listener.Accept()
		//log.Println("after Accept")
		if err != nil {
			continue
		}
		go handleClient(config, conn)
	}
}
func handleClient(cfg *config.Config, client net.Conn) {
	glog.V(2).Infoln("[proxy] handleClient start")

	err := connect(cfg, client)
	if err != nil {
		glog.Errorln("client could not authenticate and connect")
		return
	}

	defer client.Close()

	masterBuf := make([]byte, 4096)
	var writeLen int
	var readLen int
	var msgType string
	var writeCase = false
	var reqLen int
	var nextNode *config.Node
	var backendConn *net.TCPConn
	var poolIndex int

	for {

		reqLen, err = client.Read(masterBuf)
		if err != nil {
			glog.Errorln("[proxy] error reading from client conn" + err.Error())
			return
		}

		msgType = ProtocolMsgType(masterBuf)
		LogProtocol("-->", "", masterBuf, reqLen)

		glog.V(2).Infoln("here is a new msgType=" + msgType)

		//
		// adapt inbound data
		//err = cfg.Adapter.Do(&masterBuf, reqLen)
		//if err != nil {
		//log.Println("[proxy] error adapting inbound")
		//return
		//}

		//copy(replicaBuf, masterBuf)

		if msgType == "X" {
			glog.V(2).Infoln("termination msg received")
			return
		} else if msgType == "Q" {
			poolIndex = -1
			writeCase = IsWriteAnno(masterBuf)
			nextNode, err = cfg.GetNextNode(writeCase)
			if err != nil {
				glog.Errorln(err.Error())
				return
			}
			//get pool index from pool channel
			poolIndex = <-nextNode.Pool.Channel

			glog.V(2).Infof("query sending to %s pool Index=%d\n", nextNode.IPAddr, poolIndex)
			backendConn = nextNode.Pool.Connections[poolIndex]

			nextNode.Stats.Queries = nextNode.Stats.Queries + 1

			writeLen, err = backendConn.Write(masterBuf[:reqLen])
			glog.V(2).Infof("wrote outbuf reqLen=%d writeLen=%d\n", reqLen, writeLen)
			glog.V(2).Infof("read masterBuf readLen=%d\n", readLen)
			if err != nil {
				glog.Errorln(err.Error())
				glog.Errorln("[proxy] error here")
			}
			readLen, err = backendConn.Read(masterBuf)
			if poolIndex != -1 {
				ReturnConnection(nextNode.Pool.Channel, poolIndex)
			}

			if err != nil {
				glog.Errorln(err.Error())
				glog.Errorln("attempting retry of query...")
				//right here is where retry logic occurs
				//mark as unhealthy the current node
				config.UpdateHealth(nextNode, false)

				//get next node as usual
				nextNode, err = cfg.GetNextNode(writeCase)
				if err != nil {
					glog.Errorln("could not get node for query retry")
					glog.Errorln(err.Error())
				} else {
					writeLen, err = nextNode.Pool.Connections[0].Write(masterBuf[:reqLen])
					readLen, err = nextNode.Pool.Connections[0].Read(masterBuf)
					if err != nil {
						glog.Errorln("query retry failed")
						glog.Errorln(err.Error())
					}
				}
			}

			writeLen, err = client.Write(masterBuf[:readLen])
			if err != nil {
				glog.V(2).Infoln("[proxy] closing client conn" + err.Error())
				return
			}

			glog.V(2).Infof("[proxy] wrote1 to pg client %d\n", writeLen)
		} else {

			glog.V(2).Infoln("XXXX msgType here is " + msgType)

			writeLen, err = cfg.Master.TCPConn.Write(masterBuf[:reqLen])
			readLen, err = cfg.Master.TCPConn.Read(masterBuf)
			if err != nil {
				glog.Errorln("master WriteRead error:" + err.Error())
			}

			msgType = ProtocolMsgType(masterBuf)

			//write to client only the master response
			writeLen, err = client.Write(masterBuf[:readLen])
			if err != nil {
				glog.Errorln("[proxy] closing client conn" + err.Error())
				return
			}

			glog.V(2).Infof("[proxy] wrote3 to pg client %d\n", writeLen)
		}

		//err = cfg.Adapter.Do(&masterBuf, readLen) //adapt the outbound msg
		//if err != nil {
		//log.Println("[proxy] error adapting outbound msg")
		//log.Println(err.Error())
		//}

	}
	glog.V(2).Infoln("[proxy] closing client conn")
}
func checkError(err error) {
	if err != nil {
		glog.Fatalf("Fatal	error:	%s", err.Error())
	}
}

/**
func RecvMessage(conn *net.TCPConn, r *[]byte) (byte, error) {
	// workaround for a QueryRow bug, see exec
	if cn.saveMessageType != 0 {
		t := cn.saveMessageType
		*r = cn.saveMessageBuffer
		cn.saveMessageType = 0
		cn.saveMessageBuffer = nil
		return t, nil
	}

	var scratch [512]byte

	x := scratch[:5]
	_, err := io.ReadFull(conn, x)
	if err != nil {
		return 0, err
	}

	// read the type and length of the message that follows
	t := x[0]
	n := int(binary.BigEndian.Uint32(x[1:])) - 4
	var y []byte
	if n <= len(scratch) {
		y = scratch[:n]
	} else {
		y = make([]byte, n)
	}
	_, err = io.ReadFull(conn, y)
	if err != nil {
		return 0, err
	}
	*r = y
	return t, nil
}
*/
