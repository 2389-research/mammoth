// ABOUTME: A two-player Pong game that runs in the terminal using ANSI escape codes.
// ABOUTME: Uses raw terminal mode for input and renders at ~30 FPS with no flicker.
package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"
)

// Game constants
const (
	targetFPS      = 30
	frameDuration  = time.Second / targetFPS
	paddleHeight   = 5
	paddleChar     = "█"
	ballChar       = "●"
	winScore       = 11
	initialBallSpd = 30.0 // characters per second
	speedIncrement = 2.0  // speed boost per paddle hit
	maxBallSpeed   = 80.0
)

// Direction represents ball movement direction.
type Direction struct {
	dx, dy float64
}

// GameState holds all mutable game state.
type GameState struct {
	// Field dimensions (inside the border)
	fieldW, fieldH int

	// Paddle positions (Y coordinate of top of paddle, 0-indexed within field)
	paddle1Y, paddle2Y int

	// Ball position (float for sub-character precision)
	ballX, ballY float64

	// Ball direction (normalized-ish vector scaled by speed)
	ballDir Direction

	// Ball speed in characters per second
	ballSpeed float64

	// Scores
	score1, score2 int

	// Game flow
	paused    bool // waiting to serve
	gameOver  bool
	winner    int // 1 or 2
	quitting  bool
	serveSide int // 1 = left serves, 2 = right serves
}

// InputEvent represents a keyboard input.
type InputEvent int

const (
	InputNone InputEvent = iota
	InputW
	InputS
	InputUp
	InputDown
	InputQuit
	InputRestart
)

func main() {
	// Put terminal into raw mode.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to set raw mode: %v\n", err)
		os.Exit(1)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// Handle SIGINT/SIGTERM gracefully.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Hide cursor and clear screen.
	fmt.Print("\033[?25l") // hide cursor
	fmt.Print("\033[2J")   // clear screen
	defer func() {
		fmt.Print("\033[?25h") // show cursor
		fmt.Print("\033[2J")   // clear screen
		fmt.Print("\033[H")    // move to top-left
	}()

	// Start input reader goroutine.
	inputCh := make(chan InputEvent, 32)
	go readInput(inputCh)

	// Initialize game.
	game := newGame()

	ticker := time.NewTicker(frameDuration)
	defer ticker.Stop()

	lastTime := time.Now()

	for {
		select {
		case <-sigCh:
			return
		case <-ticker.C:
			now := time.Now()
			dt := now.Sub(lastTime).Seconds()
			lastTime = now

			// Drain all pending inputs.
			inputs := drainInputs(inputCh)

			for _, inp := range inputs {
				if inp == InputQuit {
					return
				}
				if game.gameOver && inp == InputRestart {
					game = newGame()
					continue
				}
				handleInput(&game, inp, dt)
			}

			if !game.gameOver && !game.paused {
				updateBall(&game, dt)
			}

			render(&game)
		}
	}
}

// newGame creates a fresh game state sized to the current terminal.
func newGame() GameState {
	w, h := getTermSize()

	// Field is inside the border, so subtract 2 for left/right walls
	// and 2 for top border + score line and bottom border.
	fieldW := w - 2
	fieldH := h - 3 // 1 for score line, 1 for top border, 1 for bottom border

	// Clamp to reasonable sizes.
	if fieldW > 120 {
		fieldW = 120
	}
	if fieldH > 40 {
		fieldH = 40
	}
	if fieldW < 20 {
		fieldW = 20
	}
	if fieldH < 10 {
		fieldH = 10
	}

	game := GameState{
		fieldW:    fieldW,
		fieldH:    fieldH,
		paddle1Y:  fieldH/2 - paddleHeight/2,
		paddle2Y:  fieldH/2 - paddleHeight/2,
		ballSpeed: initialBallSpd,
		paused:    true,
		serveSide: 1,
	}

	resetBall(&game)
	return game
}

// resetBall places the ball at center and gives it a random-ish direction.
func resetBall(g *GameState) {
	g.ballX = float64(g.fieldW) / 2.0
	g.ballY = float64(g.fieldH) / 2.0
	g.ballSpeed = initialBallSpd
	g.paused = true

	// Direction will be set when serve happens.
}

// serveBall starts the ball moving toward the appropriate side.
func serveBall(g *GameState) {
	angle := (rand.Float64()*0.8 - 0.4) * math.Pi // -0.4π to 0.4π range

	dx := math.Cos(angle)
	dy := math.Sin(angle)

	// Serve toward the side that lost the last point.
	if g.serveSide == 1 {
		dx = -math.Abs(dx)
	} else {
		dx = math.Abs(dx)
	}

	g.ballDir = Direction{dx: dx, dy: dy}
	g.paused = false
}

// handleInput processes a single input event for paddle movement.
func handleInput(g *GameState, inp InputEvent, dt float64) {
	if g.paused && (inp == InputW || inp == InputS || inp == InputUp || inp == InputDown) {
		serveBall(g)
	}

	paddleSpeed := 2 // rows per input event

	switch inp {
	case InputW:
		g.paddle1Y -= paddleSpeed
		if g.paddle1Y < 0 {
			g.paddle1Y = 0
		}
	case InputS:
		g.paddle1Y += paddleSpeed
		if g.paddle1Y+paddleHeight > g.fieldH {
			g.paddle1Y = g.fieldH - paddleHeight
		}
	case InputUp:
		g.paddle2Y -= paddleSpeed
		if g.paddle2Y < 0 {
			g.paddle2Y = 0
		}
	case InputDown:
		g.paddle2Y += paddleSpeed
		if g.paddle2Y+paddleHeight > g.fieldH {
			g.paddle2Y = g.fieldH - paddleHeight
		}
	}
}

// updateBall moves the ball and handles collisions.
func updateBall(g *GameState, dt float64) {
	g.ballX += g.ballDir.dx * g.ballSpeed * dt
	g.ballY += g.ballDir.dy * g.ballSpeed * dt

	// Top/bottom wall bounce.
	if g.ballY < 0 {
		g.ballY = -g.ballY
		g.ballDir.dy = -g.ballDir.dy
	}
	if g.ballY >= float64(g.fieldH)-1 {
		g.ballY = 2*(float64(g.fieldH)-1) - g.ballY
		g.ballDir.dy = -g.ballDir.dy
	}

	// Left paddle collision (paddle is at x=1).
	if g.ballX <= 2 && g.ballDir.dx < 0 {
		ballRow := int(math.Round(g.ballY))
		if ballRow >= g.paddle1Y && ballRow < g.paddle1Y+paddleHeight {
			g.ballX = 2
			g.ballDir.dx = -g.ballDir.dx

			// Add spin based on where ball hits paddle.
			hitPos := (g.ballY - float64(g.paddle1Y)) / float64(paddleHeight)
			g.ballDir.dy = (hitPos - 0.5) * 2.0

			// Speed up.
			g.ballSpeed += speedIncrement
			if g.ballSpeed > maxBallSpeed {
				g.ballSpeed = maxBallSpeed
			}
		}
	}

	// Right paddle collision (paddle is at x=fieldW-2).
	if g.ballX >= float64(g.fieldW)-3 && g.ballDir.dx > 0 {
		ballRow := int(math.Round(g.ballY))
		if ballRow >= g.paddle2Y && ballRow < g.paddle2Y+paddleHeight {
			g.ballX = float64(g.fieldW) - 3
			g.ballDir.dx = -g.ballDir.dx

			// Add spin based on where ball hits paddle.
			hitPos := (g.ballY - float64(g.paddle2Y)) / float64(paddleHeight)
			g.ballDir.dy = (hitPos - 0.5) * 2.0

			// Speed up.
			g.ballSpeed += speedIncrement
			if g.ballSpeed > maxBallSpeed {
				g.ballSpeed = maxBallSpeed
			}
		}
	}

	// Score: ball went past left edge.
	if g.ballX < 0 {
		g.score2++
		g.serveSide = 2
		if g.score2 >= winScore {
			g.gameOver = true
			g.winner = 2
		} else {
			resetBall(g)
		}
	}

	// Score: ball went past right edge.
	if g.ballX >= float64(g.fieldW) {
		g.score1++
		g.serveSide = 1
		if g.score1 >= winScore {
			g.gameOver = true
			g.winner = 1
		} else {
			resetBall(g)
		}
	}
}

// render draws the entire game frame to the terminal.
func render(g *GameState) {
	termW, termH := getTermSize()

	// Total frame dimensions (border + field).
	frameW := g.fieldW + 2 // +2 for left/right border
	frameH := g.fieldH + 3 // +1 score line, +1 top border, +1 bottom border

	// Offsets to center the frame.
	offsetX := (termW - frameW) / 2
	if offsetX < 0 {
		offsetX = 0
	}
	offsetY := (termH - frameH) / 2
	if offsetY < 0 {
		offsetY = 0
	}

	var buf strings.Builder
	buf.Grow(frameW * frameH * 4) // rough estimate

	// Move cursor to top-left, clear wouldn't be needed since we overwrite everything.
	buf.WriteString("\033[H")

	centerLine := g.fieldW / 2

	for row := 0; row < frameH; row++ {
		// Move cursor to the correct position for this row.
		screenRow := offsetY + row + 1
		screenCol := offsetX + 1
		buf.WriteString(fmt.Sprintf("\033[%d;%dH", screenRow, screenCol))

		if row == 0 {
			// Score line.
			scoreLine := fmt.Sprintf("Player 1: %d    Player 2: %d", g.score1, g.score2)
			pad := (frameW - len(scoreLine)) / 2
			if pad < 0 {
				pad = 0
			}
			buf.WriteString(strings.Repeat(" ", pad))
			buf.WriteString("\033[1;37m") // bold white
			buf.WriteString(scoreLine)
			buf.WriteString("\033[0m")
			buf.WriteString(strings.Repeat(" ", frameW-pad-len(scoreLine)))
			continue
		}

		if row == 1 {
			// Top border.
			buf.WriteString("┌")
			buf.WriteString(strings.Repeat("─", g.fieldW))
			buf.WriteString("┐")
			continue
		}

		if row == frameH-1 {
			// Bottom border.
			buf.WriteString("└")
			buf.WriteString(strings.Repeat("─", g.fieldW))
			buf.WriteString("┘")
			continue
		}

		// Field row (row-2 maps to field row 0).
		fieldRow := row - 2

		buf.WriteString("│")

		ballCol := int(math.Round(g.ballX))
		ballRow := int(math.Round(g.ballY))

		for col := 0; col < g.fieldW; col++ {
			// Check what to draw at this cell.
			switch {
			case col == 1 && fieldRow >= g.paddle1Y && fieldRow < g.paddle1Y+paddleHeight:
				// Left paddle.
				buf.WriteString("\033[1;34m") // bold blue
				buf.WriteString(paddleChar)
				buf.WriteString("\033[0m")

			case col == g.fieldW-2 && fieldRow >= g.paddle2Y && fieldRow < g.paddle2Y+paddleHeight:
				// Right paddle.
				buf.WriteString("\033[1;31m") // bold red
				buf.WriteString(paddleChar)
				buf.WriteString("\033[0m")

			case col == ballCol && fieldRow == ballRow && !g.gameOver:
				// Ball.
				buf.WriteString("\033[1;33m") // bold yellow
				buf.WriteString(ballChar)
				buf.WriteString("\033[0m")

			case col == centerLine && fieldRow%2 == 0:
				// Center line (dashed).
				buf.WriteString("\033[2;37m") // dim white
				buf.WriteString("│")
				buf.WriteString("\033[0m")

			default:
				buf.WriteString(" ")
			}
		}

		buf.WriteString("│")
	}

	// Status line below the field.
	statusRow := offsetY + frameH + 1
	statusCol := offsetX + 1
	buf.WriteString(fmt.Sprintf("\033[%d;%dH", statusRow, statusCol))
	buf.WriteString("\033[K") // clear line

	if g.gameOver {
		msg := fmt.Sprintf("Player %d wins! Press R to play again, Q to quit.", g.winner)
		pad := (frameW - len(msg)) / 2
		if pad < 0 {
			pad = 0
		}
		buf.WriteString(strings.Repeat(" ", pad))
		buf.WriteString("\033[1;32m") // bold green
		buf.WriteString(msg)
		buf.WriteString("\033[0m")
	} else if g.paused {
		msg := "Press any movement key to serve..."
		pad := (frameW - len(msg)) / 2
		if pad < 0 {
			pad = 0
		}
		buf.WriteString(strings.Repeat(" ", pad))
		buf.WriteString("\033[2;37m") // dim white
		buf.WriteString(msg)
		buf.WriteString("\033[0m")
	} else {
		// Show controls.
		msg := "P1: W/S  |  P2: ↑/↓  |  Q: Quit"
		pad := (frameW - len(msg)) / 2
		if pad < 0 {
			pad = 0
		}
		buf.WriteString(strings.Repeat(" ", pad))
		buf.WriteString("\033[2;37m")
		buf.WriteString(msg)
		buf.WriteString("\033[0m")
	}

	fmt.Print(buf.String())
}

// readInput reads raw bytes from stdin and sends InputEvents to the channel.
func readInput(ch chan<- InputEvent) {
	buf := make([]byte, 32)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		for i := 0; i < n; {
			b := buf[i]

			switch {
			case b == 0x1b: // Escape sequence
				if i+2 < n && buf[i+1] == '[' {
					switch buf[i+2] {
					case 'A': // Up arrow
						ch <- InputUp
					case 'B': // Down arrow
						ch <- InputDown
					}
					i += 3
					continue
				}
				// Bare escape = quit.
				ch <- InputQuit
				i++

			case b == 'q' || b == 'Q':
				ch <- InputQuit
				i++

			case b == 'w' || b == 'W':
				ch <- InputW
				i++

			case b == 's' || b == 'S':
				ch <- InputS
				i++

			case b == 'r' || b == 'R':
				ch <- InputRestart
				i++

			case b == 3: // Ctrl+C
				ch <- InputQuit
				i++

			default:
				i++
			}
		}
	}
}

// drainInputs reads all currently pending input events from the channel.
func drainInputs(ch <-chan InputEvent) []InputEvent {
	var inputs []InputEvent
	for {
		select {
		case inp := <-ch:
			inputs = append(inputs, inp)
		default:
			return inputs
		}
	}
}

// termSize cache to avoid too many syscalls.
var (
	termSizeMu    sync.Mutex
	cachedW       int
	cachedH       int
	lastSizeCheck time.Time
)

// getTermSize returns the current terminal dimensions, cached briefly.
func getTermSize() (int, int) {
	termSizeMu.Lock()
	defer termSizeMu.Unlock()

	if time.Since(lastSizeCheck) < 500*time.Millisecond && cachedW > 0 {
		return cachedW, cachedH
	}

	w, h, err := term.GetSize(int(os.Stdin.Fd()))
	if err != nil || w <= 0 || h <= 0 {
		// Fallback to reasonable defaults.
		return 80, 24
	}

	cachedW = w
	cachedH = h
	lastSizeCheck = time.Now()

	return w, h
}
