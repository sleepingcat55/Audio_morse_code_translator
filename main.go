package main

import (
	"flag"
	"fmt"
	"io"
	"math"
	"runtime"
	"sync"
	"time"
	"strings"
	"regexp"
	"os"
	"bufio"

	"github.com/ebitengine/oto/v3"
)

var (
	sampleRate   = flag.Int("samplerate", 48000, "sample rate")
	channelCount = flag.Int("channelcount", 2, "number of channel")
	format       = flag.String("format", "s16le", "source format (u8, s16le, or f32le)")
)

type SineWave struct {
	freq   float64
	length int64
	pos    int64

	channelCount int
	format       oto.Format

	remaining []byte
}

func formatByteLength(format oto.Format) int {
	switch format {
	case oto.FormatFloat32LE:
		return 4
	case oto.FormatUnsignedInt8:
		return 1
	case oto.FormatSignedInt16LE:
		return 2
	default:
		panic(fmt.Sprintf("unexpected format: %d", format))
	}
}

func NewSineWave(freq float64, duration time.Duration, channelCount int, format oto.Format) *SineWave {
	l := int64(channelCount) * int64(formatByteLength(format)) * int64(*sampleRate) * int64(duration) / int64(time.Second)
	l = l / 4 * 4
	return &SineWave{
		freq:         freq,
		length:       l,
		channelCount: channelCount,
		format:       format,
	}
}

func (s *SineWave) Read(buf []byte) (int, error) {
	if len(s.remaining) > 0 {
		n := copy(buf, s.remaining)
		copy(s.remaining, s.remaining[n:])
		s.remaining = s.remaining[:len(s.remaining)-n]
		return n, nil
	}

	if s.pos == s.length {
		return 0, io.EOF
	}

	eof := false
	if s.pos+int64(len(buf)) > s.length {
		buf = buf[:s.length-s.pos]
		eof = true
	}

	var origBuf []byte
	if len(buf)%4 > 0 {
		origBuf = buf
		buf = make([]byte, len(origBuf)+4-len(origBuf)%4)
	}

	length := float64(*sampleRate) / float64(s.freq)

	num := formatByteLength(s.format) * s.channelCount
	p := s.pos / int64(num)
	switch s.format {
	case oto.FormatFloat32LE:
		for i := 0; i < len(buf)/num; i++ {
			bs := math.Float32bits(float32(math.Sin(2*math.Pi*float64(p)/length) * 0.3))
			for ch := 0; ch < *channelCount; ch++ {
				buf[num*i+4*ch] = byte(bs)
				buf[num*i+1+4*ch] = byte(bs >> 8)
				buf[num*i+2+4*ch] = byte(bs >> 16)
				buf[num*i+3+4*ch] = byte(bs >> 24)
			}
			p++
		}
	case oto.FormatUnsignedInt8:
		for i := 0; i < len(buf)/num; i++ {
			const max = 127
			b := int(math.Sin(2*math.Pi*float64(p)/length) * 0.3 * max)
			for ch := 0; ch < *channelCount; ch++ {
				buf[num*i+ch] = byte(b + 128)
			}
			p++
		}
	case oto.FormatSignedInt16LE:
		for i := 0; i < len(buf)/num; i++ {
			const max = 32767
			b := int16(math.Sin(2*math.Pi*float64(p)/length) * 0.3 * max)
			for ch := 0; ch < *channelCount; ch++ {
				buf[num*i+2*ch] = byte(b)
				buf[num*i+1+2*ch] = byte(b >> 8)
			}
			p++
		}
	}

	s.pos += int64(len(buf))

	n := len(buf)
	if origBuf != nil {
		n = copy(origBuf, buf)
		s.remaining = buf[n:]
	}

	if eof {
		return n, io.EOF
	}
	return n, nil
}

func play(context *oto.Context, freq float64, duration time.Duration, channelCount int, format oto.Format) *oto.Player {
	p := context.NewPlayer(NewSineWave(freq, duration, channelCount, format))
	p.Play()
	return p
}

func run(a_coder_audio string) error {
	const (
		freqC = 523.3
		freqE = 659.3
		freqG = 784.0
	)

	op := &oto.NewContextOptions{}
	op.SampleRate = *sampleRate
	op.ChannelCount = *channelCount

	switch *format {
	case "f32le":
		op.Format = oto.FormatFloat32LE
	case "u8":
		op.Format = oto.FormatUnsignedInt8
	case "s16le":
		op.Format = oto.FormatSignedInt16LE
	default:
		return fmt.Errorf("format must be u8, s16le, or f32le but: %s", *format)
	}
	c, ready, err := oto.NewContext(op)
	if err != nil {
		return err
	}
	<-ready

	var wg sync.WaitGroup
	var players []*oto.Player
	var m sync.Mutex

	wg.Add(1)
	go func() {
		defer wg.Done()

		for i:=0; i<len(a_coder_audio); i++ {
			if string(a_coder_audio[i]) == "." {
				p := play(c, freqC, ((1*time.Second)/5), op.ChannelCount, op.Format)
				m.Lock()
				players = append(players, p)
				m.Unlock()
				time.Sleep((1*time.Second)/2)
			} else if string(a_coder_audio[i]) == "-" {
				p := play(c, freqC, ((1*time.Second)/2), op.ChannelCount, op.Format)
				m.Lock()
				players = append(players, p)
				m.Unlock()
				time.Sleep((1*time.Second)/2)
			}
			time.Sleep((1*time.Second)/2)
		}
		time.Sleep(1 * time.Second)
	}()

	wg.Wait()

	// Pin the players not to GC the players.
	runtime.KeepAlive(players)

	return nil
}

var (
	alphabet = [36]string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z", "1", "2", "3", "4", "5", "6", "7", "8", "9", "0"}
	at_morse = [36]string{".-", "-...", "-.-.", "-..", ".", "..-.", "--.", "....", "..", ".---", "-.-", ".-..", "--", "-.", "---", ".--.", "--.-", ".-.", "...", "-", "..-", "...-", ".--", "-..-", "-.--", "--..", ".----", "..---", "...--", "....-", ".....", "-....", "--...", "---..", "----.", "-----"}
)
var inputReader *bufio.Reader
var inputt string
var err error

func codage_morse(mot_a_coder string) string {
	var resu string
	for i:=0; i<len(mot_a_coder); i++ {
		for j:=0; j<len(alphabet); j++ {
			if string(mot_a_coder[i]) == alphabet[j] {
				resu += at_morse[j]	
				resu += "/"	
			} else if string(mot_a_coder[i]) == " " {
				resu += "_"	// permet de simuler un espace
				// on va essayer d'eviter les 26 espaces
				previous_lettre := len(resu)-1
				if string(resu[previous_lettre]) == "_" {
					break
				}
			}
			
		}
	}
	return resu
}

func decodage_morse(mot_a_decoder string) string {
	var resu string
	var tempo string
	var entmpo string
	for i:=0; i<len(mot_a_decoder); i++ {
		// fmt.Printf("%c\n", mot_a_decoder[i])
		tempo += string(mot_a_decoder[i])
		
		if string(mot_a_decoder[i]) == "/" {
			re, _ := regexp.Compile("/")
			entmpo = re.ReplaceAllString(tempo, "")
			// fmt.Printf("valeur de entmpo dans le if : %s\n", entmpo)

			for j:=0; j<len(at_morse); j++ {
				if entmpo == at_morse[j] {
					resu += alphabet[j]	
					entmpo = ""
					tempo = ""
				}
			}
		} else if string(mot_a_decoder[i]) == "_" {
			tempo = ""
			resu += " "
		} 
	}
	return resu
}


func main() {
	inputReader = bufio.NewReader(os.Stdin)
	fmt.Println("Your morse sentence : ")
	inputt, err = inputReader.ReadString('\n')
	if err == nil {
		fmt.Printf("Input : %s\n", inputt)
	}

	mot_to_coder := strings.ToLower(inputt)

	codage := codage_morse(mot_to_coder)
	fmt.Printf("Your sentence : %s\n", codage)

	decodage := decodage_morse(codage)
	fmt.Printf("Your sentence : %s\n", decodage)

	flag.Parse()
	if err := run(codage); err != nil {
		panic(err)
	}
}
