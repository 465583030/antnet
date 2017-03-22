package antnet

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

func Print(a ...interface{}) (int, error) {
	return fmt.Print(a...)
}
func Println(a ...interface{}) (int, error) {
	return fmt.Println(a...)
}
func Printf(format string, a ...interface{}) (int, error) {
	return fmt.Printf(format, a...)
}
func Sprintf(format string, a ...interface{}) string {
	return fmt.Sprintf(format, a...)
}
func Stop() {
	if !atomic.CompareAndSwapInt32(&stop, 0, 1) {
		return
	}

	for _, v := range msgqueMap {
		v.Stop()
	}

	stopMapLock.Lock()
	for k, v := range stopMap {
		close(v)
		delete(stopMap, k)
	}
	stopMapLock.Unlock()

	stopChan <- nil

	for _, v := range redisManagers {
		v.close()
	}

	LogInfo("Server Stop")
	waitAll.Wait()

	if !atomic.CompareAndSwapInt32(&stopForLog, 0, 1) {
		return
	}

	stopMapForLogLock.Lock()
	for k, v := range stopMapForLog {
		close(v)
		delete(stopMapForLog, k)
	}
	stopMapForLogLock.Unlock()
	waitAllForLog.Wait()
}

func IsStop() bool {
	return stop == 1
}

func IsRuning() bool {
	return stop == 0
}

func Now() time.Time {
	return time.Now()
}

func CmdAct(cmd, act uint8) int {
	return int(cmd<<8 + act)
}

func MD5Str(s string) string {
	md5Ctx := md5.New()
	md5Ctx.Write([]byte(s))
	cipherStr := md5Ctx.Sum(nil)
	return hex.EncodeToString(cipherStr)
}

func Go(fn func()) {
	waitAll.Add(1)
	LogDebug("goroutine count:%d", atomic.AddInt32(&gocount, 1))
	go func() {
		fn()
		waitAll.Done()
		LogDebug("goroutine count:%d", atomic.AddInt32(&gocount, ^int32(0)))
	}()
}

func GoArgs(fn func(...interface{}), args ...interface{}) {
	waitAll.Add(1)
	LogDebug("goroutine count:%d", atomic.AddInt32(&gocount, 1))
	go func() {
		fn(args...)
		waitAll.Done()
		LogDebug("goroutine count:%d", atomic.AddInt32(&gocount, ^int32(0)))
	}()
}

func Go2(fn func(cstop chan struct{})) bool {
	if IsStop() {
		return false
	}
	waitAll.Add(1)
	LogDebug("goroutine count:%d", atomic.AddInt32(&gocount, 1))
	go func() {
		id := atomic.AddUint64(&goId, 1)
		cstop := make(chan struct{})
		stopMapLock.Lock()
		stopMap[id] = cstop
		stopMapLock.Unlock()
		fn(cstop)

		stopMapLock.Lock()
		if _, ok := stopMap[id]; ok {
			close(cstop)
			delete(stopMap, id)
		}
		stopMapLock.Unlock()

		waitAll.Done()
		LogDebug("goroutine count:%d", atomic.AddInt32(&gocount, ^int32(0)))
	}()
	return true
}

func goForLog(fn func(cstop chan struct{})) bool {
	if IsStop() {
		return false
	}
	waitAllForLog.Add(1)

	go func() {
		id := atomic.AddUint64(&goId, 1)
		cstop := make(chan struct{})
		stopMapForLogLock.Lock()
		stopMapForLog[id] = cstop
		stopMapForLogLock.Unlock()
		fn(cstop)

		stopMapForLogLock.Lock()
		if _, ok := stopMapForLog[id]; ok {
			close(cstop)
			delete(stopMapForLog, id)
		}
		stopMapForLogLock.Unlock()

		waitAllForLog.Done()
	}()
	return true
}

func WaitForSystemExit() {
	statis.StartTime = time.Now()
	stopChan = make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, os.Kill, syscall.SIGTERM)
	for stop == 0 {
		select {
		case <-stopChan:
			Stop()
		}
	}
	Stop()
}

func PathExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return false
}

func Daemon(skip []string) {
	if os.Getppid() != 1 {
		filePath, _ := filepath.Abs(os.Args[0])
		newCmd := []string{}
		for _, v := range os.Args {
			add := true
			for _, s := range skip {
				if strings.Contains(v, s) {
					add = false
					break
				}
			}
			if add {
				newCmd = append(newCmd, v)
			}
		}
		cmd := exec.Command(filePath)
		cmd.Args = newCmd
		cmd.Start()
	}
}

func LogStack() {
	buf := make([]byte, 1<<12)
	LogError(string(buf[:runtime.Stack(buf, false)]))
}

func GetStatis() *Statis {
	statis.GoCount = int(gocount)
	statis.MsgqueCount = len(msgqueMap)
	return &statis
}

func Atoi(str string) int {
	i, err := strconv.Atoi(str)
	if err != nil {
		return 0
	}
	return i
}

func Itoa(num int) string {
	return strconv.Itoa(num)
}

func GetSelfIp() (ips []string) {
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			if ipnet.IP.To4() != nil {
				ips = append(ips, ipnet.IP.String())
			}
		}
	}
	return
}

func GetSelfIntraIp() (ips []string) {
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			if ipnet.IP.To4() != nil {
				if ipnet.IP.IsLoopback() {
					ips = append(ips, ipnet.IP.String())
				} else {
					ipA := strings.Split(ipnet.IP.String(), ".")[0]
					if ipA == "10" || ipA == "172" || ipA == "192" {
						ips = append(ips, ipnet.IP.String())
					}
				}
			}
		}
	}
	return
}

func ReadFile(path string) ([]byte, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, ErrFileRead
	}
	return data, nil
}

func SplitStr(s string, sep string) []string {
	return strings.Split(s, sep)
}

func TrimStr(s string) string {
	return strings.TrimSpace(s)
}

func GetSelfExtraIp() (ips []string) {
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			if ipnet.IP.To4() != nil {
				if ipnet.IP.IsLoopback() {
					continue
				} else {
					ipA := strings.Split(ipnet.IP.String(), ".")[0]
					if ipA == "10" || ipA == "172" || ipA == "192" {
						continue
					}
					ips = append(ips, ipnet.IP.String())
				}
			}
		}
	}
	return
}

func ParseBaseKind(kind reflect.Kind, data string) (interface{}, error) {
	switch kind {
	case reflect.String:
		return data, nil
	case reflect.Bool:
		v := data == "1" || data == "true"
		return v, nil
	case reflect.Int:
		x, err := strconv.ParseInt(data, 0, 64)
		return int(x), err
	case reflect.Int8:
		x, err := strconv.ParseInt(data, 0, 8)
		return int8(x), err
	case reflect.Int16:
		x, err := strconv.ParseInt(data, 0, 16)
		return int16(x), err
	case reflect.Int32:
		x, err := strconv.ParseInt(data, 0, 32)
		return int32(x), err
	case reflect.Int64:
		x, err := strconv.ParseInt(data, 0, 64)
		return int64(x), err
	case reflect.Float32:
		x, err := strconv.ParseFloat(data, 32)
		return float32(x), err
	case reflect.Float64:
		x, err := strconv.ParseFloat(data, 64)
		return float64(x), err
	case reflect.Uint:
		x, err := strconv.ParseUint(data, 10, 64)
		return uint(x), err
	case reflect.Uint8:
		x, err := strconv.ParseUint(data, 10, 8)
		return uint8(x), err
	case reflect.Uint16:
		x, err := strconv.ParseUint(data, 10, 16)
		return uint16(x), err
	case reflect.Uint32:
		x, err := strconv.ParseUint(data, 10, 32)
		return uint32(x), err
	case reflect.Uint64:
		x, err := strconv.ParseUint(data, 10, 64)
		return uint64(x), err
	default:
		return nil, errors.New("type not found")
	}
}
