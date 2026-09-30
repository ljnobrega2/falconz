package obs

import (
	"io"
	"log/slog"
	"os"
)

// LoggerOptions configura NewLogger. Zero-value é seguro (nível Info, stdout).
type LoggerOptions struct {
	// Level mínimo logado. Zero-value = slog.LevelInfo.
	Level slog.Level
	// Writer de saída. nil = os.Stdout.
	Writer io.Writer
	// Service, quando não-vazio, é anexado como atributo "service" em todo log.
	Service string
	// AddSource inclui arquivo:linha no log (mais caro — usar em debug).
	AddSource bool
}

// NewLogger cria um *slog.Logger JSON estruturado padronizado para os serviços.
// Função PURA: não toca em slog.Default — testes capturam um buffer e validam o
// shape JSON sem efeito colateral global. Use SetupDefault para aplicar no padrão.
func NewLogger(opts LoggerOptions) *slog.Logger {
	w := opts.Writer
	if w == nil {
		w = os.Stdout
	}
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:     opts.Level,
		AddSource: opts.AddSource,
	})
	logger := slog.New(h)
	if opts.Service != "" {
		logger = logger.With("service", opts.Service)
	}
	return logger
}

// SetupDefault cria o logger com NewLogger e o instala como slog.Default,
// devolvendo-o também. Atalho para o main.go de cada serviço:
//
//	logger := obs.SetupDefault(obs.LoggerOptions{Service: "wallet"})
//
// É o ÚNICO ponto deste pacote que muta estado global — por escolha explícita do
// chamador, nunca como efeito colateral de NewLogger.
func SetupDefault(opts LoggerOptions) *slog.Logger {
	logger := NewLogger(opts)
	slog.SetDefault(logger)
	return logger
}
