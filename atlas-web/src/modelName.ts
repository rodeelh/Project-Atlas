/**
 * parseModelInfo — extracts a human-readable display name and quantization
 * tag from a GGUF filename.
 *
 * Examples:
 *   "qwen2.5-3b-instruct-q4_k_m.gguf"  → { display: "Qwen 2.5 3B Instruct", quant: "Q4_K_M" }
 *   "gemma-3-4b-it-Q4_K_M.gguf"        → { display: "Gemma 3 4B It",         quant: "Q4_K_M" }
 *   "phi-4-mini-instruct-Q4_K_M.gguf"  → { display: "Phi 4 Mini Instruct",   quant: "Q4_K_M" }
 *   "llama3.2"                          → { display: "Llama3.2",              quant: null }
 */
export function parseModelInfo(filename: string): { display: string; quant: string | null } {
  const base = filename.replace(/\.gguf$/i, '')
  // Match common quant patterns at the end: Q4_K_M, Q8_0, IQ2_M, IQ3_XS, F16, BF16, etc.
  const quantRe = /[-_]((?:IQ|Q)\d+[_A-Za-z0-9]*|[BF]16|f16|bf16)$/i
  const quantMatch = base.match(quantRe)
  const quant = quantMatch ? quantMatch[1].toUpperCase() : null
  const nameBase = quantMatch ? base.slice(0, quantMatch.index) : base
  const display = nameBase
    .replace(/[-_.]/g, ' ')
    .replace(/\s+/g, ' ')
    .replace(/\b(\w)/g, c => c.toUpperCase())
    .trim()
  return { display, quant }
}

/**
 * formatAtlasModelName — returns a single display string combining the
 * parsed name and quantization tag, suitable for compact UI labels.
 *
 * "qwen2.5-3b-instruct-q4_k_m.gguf" → "Qwen 2.5 3B Instruct · Q4_K_M"
 * "gemma-3-4b-it-Q4_K_M.gguf"       → "Gemma 3 4B It · Q4_K_M"
 * "llama3.2"                          → "Llama3.2"
 */
export function formatAtlasModelName(filename: string): string {
  if (!filename) return filename
  const { display, quant } = parseModelInfo(filename)
  return quant ? `${display} · ${quant}` : display
}
