// POCSAG code adapted from https://github.com/F5OEO/rpitx/blob/8a5fd8be49370e10bb1e23a17562e7b4bde0b81d/src/pocsag/pocsag.cpp

/*
TODO
- Trigger GPIO to enable TX
- Add API or message buss for messages
*/

package main

import (
	"fmt"
	"log"
	"math/bits"

	"github.com/hajimehoshi/oto"
)

const (
	preambleLength     int    = 576        // before message, alternating 1,0 bits
	frameSync          uint32 = 0x7CD215D8 // start of every batch
	batchSize          int    = 16         // batch is 8 frames 16 words
	frameSize          int    = 2          // consists of a pair of two words
	idle               uint32 = 0x7A89C197
	addressFlag        uint32 = 0x000000 // 1st bit of a address code word
	messageFlag        uint32 = 0x100000 // 1st bit of message code word
	textDataType       int    = 0x3      // last two bits of an address word's data represent the data type
	numericDataType    int    = 0x0
	bitsPerWord        int    = 20
	bitsPerCharText    int    = 7
	bitsPerCharNumeric int    = 4
	bitsCRC            int    = 10
	crcGenorator       uint32 = 0b11101101001
)

func main() {

	address := 1001
	funcBits := 3
	message := "test 1 2 3 4 5"
	rate := 1200 //(512/1200/2400)
	inverted := false

	encoded, err := encodeTransmission(address, funcBits, message)
	if err != nil {
		log.Fatal(err)
	}

	for _, n := range encoded {
		fmt.Printf("%032b \n", n)
	}

	err = playBits(rate, inverted, encoded)
	if err != nil {
		log.Fatal(err)
	}
}

func encodeTransmission(address int, funcBits int, message string) ([]uint32, error) {
	var out []uint32

	// premble
	for i := 0; i < preambleLength/32; i++ {
		out = append(out, 0xAAAAAAAA)
	}

	// frame sync
	out = append(out, frameSync)

	// idle
	prefixLength := addressOffset(address)
	for i := 0; i < prefixLength; i++ {
		out = append(out, idle)
	}

	out = append(out, encodeCodeword(((uint32(address)>>3)<<2)|uint32(funcBits)))

	out = append(out, encodeASCII(addressOffset(address)+1, message)...)

	// idle
	written := len(out) - (preambleLength / 32)
	for i := 1; i < written%batchSize; i++ {
		out = append(out, idle)
	}

	return out, nil

}

func addressOffset(address int) int {
	return (address & 0x7) * frameSize
}

func encodeASCII(offset int, message string) []uint32 {
	//Data for the current word we're writing
	var currentWord uint32 = 0

	//Number of bits we've written so far to the current word
	var currentNumBits int = 0

	//Position of current word in the current batch
	var wordPosition int = offset

	var out []uint32

	for _, c := range message {
		//Encode the character bits backwards
		for i := 0; i < bitsPerCharText; i++ {
			currentWord <<= 1
			currentWord |= (uint32(c) >> i) & 1
			currentNumBits++
			if currentNumBits == bitsPerWord {
				//Add the MESSAGE flag to our current word and encode it.
				out = append(out, encodeCodeword(currentWord|messageFlag))
				currentWord = 0
				currentNumBits = 0

				wordPosition++
				if wordPosition == batchSize {
					//We've filled a full batch, time to insert a SYNC word
					//and start a new one.
					out = append(out, frameSync)
					wordPosition = 0
				}
			}
		}
	}

	//Write remainder of message
	if currentNumBits > 0 {
		//Pad out the word to 20 bits with zeroes
		currentWord <<= 20 - currentNumBits
		out = append(out, encodeCodeword(currentWord|messageFlag))

		wordPosition++
		if wordPosition == batchSize {
			//We've filled a full batch, time to insert a SYNC word
			//and start a new one.
			out = append(out, frameSync)
			wordPosition = 0
		}
	}

	return out

}

func encodeCodeword(message uint32) uint32 {
	fullCRC := (message << uint32(bitsCRC)) | crc(message)
	p := parity(fullCRC)
	return (fullCRC << 1) | p
}

func parity(message uint32) uint32 {
	return uint32(bits.OnesCount32(message) % 2)
}

func crc(message uint32) uint32 {

	//Align MSB of denominatorerator with MSB of message
	denominator := crcGenorator << 20

	//Message is right-padded with zeroes to the message length + crc length
	msg := message << bitsCRC

	//We iterate until denominator has been right-shifted back to it's original value.
	for column := 0; column <= 20; column++ {
		//Bit for the column we're aligned to
		msgBit := (msg >> (30 - column)) & 1

		//If the current bit is zero, we don't modify the message this iteration
		if msgBit != 0 {
			//While we would normally subtract in long division, we XOR here.
			msg ^= denominator
		}

		//Shift the denominator over to align with the next column
		denominator >>= 1
	}

	//At this point 'msg' contains the CRC value we've calculated
	return msg & 0x3FF
}

func playBits(rate int, inverted bool, encoded []uint32) error {
	// the idea of playing bits to the audio interface is thanks to DAPNETs implementation
	//aplay -t raw -N -f U8 -c 1 -r 48000 -D dev

	// level := 124
	// low := 127 - level
	// high := 128 + level
	sampleRate := 48000
	samplesPerBit := sampleRate / rate

	c, err := oto.NewContext(sampleRate, 1, 1, 4096)
	if err != nil {
		return err
	}
	p := c.NewPlayer()

	var buf []byte

	for _, word := range encoded {
		for i := 0; i < 32; i++ {
			bit := (word & (1 << (31 - i))) != 0
			if (!inverted && bit) || (inverted && !bit) {
				for i := 0; i < samplesPerBit; i++ {
					buf = make([]byte, 2)
					buf[1] = byte(0)
					p.Write(buf)
				}
			} else {
				for i := 0; i < samplesPerBit; i++ {
					buf = make([]byte, 2)
					buf[1] = byte(255)
					p.Write(buf)
				}
			}
		}
	}

	if err := p.Close(); err != nil {
		return err
	}

	return nil

}
