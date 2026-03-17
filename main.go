package main;

import(
    "flag"
    "fmt"
    "os"
    "strings"
    "time"
    "net"
    "sync"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"log"

)

var file string
var count int
var monitor bool
var ICMP bool
var hosts []string

func main() {
    flag.StringVar(&file, "f", "", "Path to hosts file")
    flag.IntVar(&count, "c", 1, "Concurrency level")
    flag.BoolVar(&monitor, "monitor", false, "Enable monitoring mode")
	flag.BoolVar(&ICMP, "ICMP", false, "Enable ICMP mode")
    flag.Parse()
    
    data, err := os.ReadFile(file)
    if err != nil {
        fmt.Println("Ошибка чтения:", err)
        return
    }

    lines := strings.Split(strings.TrimSpace(string(data)), "\n")
    hosts = make([]string, 0, len(lines))
    for _, line := range lines {
        host := strings.TrimSpace(line)
        if host != "" {
            hosts = append(hosts, host)
        }
    }

    if monitor {
        fmt.Println("Monitoring mode enabled\n")
        Monitoring(time.Second*5)
    } else {
        fmt.Println("Ping hosts\n")
        Ping(count)
    }
}

func Ping(count int) {
    var wg sync.WaitGroup
    sem := make(chan struct{}, count)
    
    for i := 0; i < len(hosts); i++ {
        wg.Add(1)
        sem <- struct{}{}
        go func(host string) {
            defer wg.Done()
            defer func() { <-sem }()
            
            for j := 0; j < count; j++ {
                var success bool
                var duration time.Duration
                if ICMP {
                    success, duration, _ = pingHostICMP(host)
                } else {
                    success, duration, _ = pingHost(host)
                }
                
                status := "Timeout"
                if success {
                    status = "OK"
                }
                fmt.Printf("%s | %s | %.2f ms [%d/%d]\n", 
                    host, status, duration.Seconds()*1000, j+1, count)
                time.Sleep(time.Second)
            }
        }(hosts[i])
    }
    wg.Wait()
}



func Monitoring(interval time.Duration) {
    var wg sync.WaitGroup 
    
    logFile, err := os.OpenFile("monitor.log", 
        os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        log.Fatal("Failed to open log file:", err)
    }
    defer logFile.Close()
    
    logger := log.New(logFile, "", log.LstdFlags)
    
    for _, host := range hosts {
        wg.Add(1)
        go func(h string) {
            defer wg.Done()
            ticker := time.NewTicker(interval)
            defer ticker.Stop()
            
            for range ticker.C {
                var success bool
                var duration time.Duration
                
                if ICMP {
                    success, duration, _ = pingHostICMP(h)
                } else {
                    success, duration, _ = pingHost(h)
                }
                status := "Timeout"
                if success {
                    status = "OK"
                }
                now := time.Now()
                logEntry := fmt.Sprintf("%s | %s | %s | %.0fms", 
                    now.Format("15:04:05"), h, status, duration.Seconds()*1000)
                
                fmt.Println(logEntry)
                logger.Println(logEntry)
            }
        }(host)
    }
    
    wg.Wait()
}


func pingHost(host string) (bool, time.Duration, error) {
    start := time.Now()
    
    for _, port := range []string{"80", "443"} {
        conn, err := net.DialTimeout("tcp", host+":"+port, 1*time.Second)
        if err == nil {
            conn.Close()
			duration := time.Since(start)
            return true, duration, nil
        }
    }
    return false, 0, nil
}

func pingHostICMP(host string) (bool, time.Duration, error) {
    start := time.Now()
    
    ips, err := net.LookupIP(host)
    if err != nil {
        return false, 0, fmt.Errorf("не удалось разрешить %s: %v", host, err)
    }
    
    var target net.IP
    for _, ip := range ips {
        if ip4 := ip.To4(); ip4 != nil {
            target = ip4
            break
        }
    }
    if target == nil {
        return false, 0, fmt.Errorf("%s не имеет IPv4 адреса", host)
    }
    
    c, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
    if err != nil {
        return false, 0, fmt.Errorf("не удалось создать ICMP сокет: %v", err)
    }
    defer c.Close()
    

    msg := icmp.Message{
        Type: ipv4.ICMPTypeEcho,
        Code: 0,
        Body: &icmp.Echo{
            ID:   os.Getpid() & 0xffff,
            Seq:  1,
            Data: []byte("PING"),
        },
    }
    
    if b, err := msg.Marshal(nil); err != nil {
        return false, 0, err
    } else if _, err := c.WriteTo(b, &net.IPAddr{IP: target}); err != nil {
        return false, 0, fmt.Errorf("не удалось отправить ICMP: %v", err)
    }
    
    c.SetReadDeadline(time.Now().Add(time.Second))
    
    reply := make([]byte, 1500)
    if n, _, err := c.ReadFrom(reply); err != nil {
        return false, 0, fmt.Errorf("timeout или ошибка ответа: %v", err)
    } else if msgReply, err := icmp.ParseMessage(ipv4.ICMPTypeEchoReply.Protocol(), reply[:n]); 
         err != nil || msgReply.Type != ipv4.ICMPTypeEchoReply {
        return false, 0, fmt.Errorf("неверный ответ ICMP")
    }
    
    return true, time.Since(start), nil
}