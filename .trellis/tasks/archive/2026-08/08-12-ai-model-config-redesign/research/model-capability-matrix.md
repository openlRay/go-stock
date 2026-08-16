# AI Model Capability Matrix

## Evidence base

The matrix below is based on the adapters and pinned SDK versions in this repository, not on an assumption that every OpenAI-compatible gateway accepts every OpenAI field.

- Generic OpenAI-compatible: `github.com/cloudwego/eino-ext/components/model/openai v0.1.13`
- OpenRouter: `github.com/cloudwego/eino-ext/components/model/openrouter v0.1.10`
- Ark: `github.com/cloudwego/eino-ext/components/model/ark v0.1.68`
- Claude: `github.com/cloudwego/eino-ext/components/model/claude v0.1.23`
- Gemini: `github.com/cloudwego/eino-ext/components/model/gemini v0.1.33` and `google.golang.org/genai v1.60.0`
- DeepSeek: `github.com/cloudwego/eino-ext/components/model/deepseek v0.1.7`
- Qwen: `github.com/cloudwego/eino-ext/components/model/qwen v0.1.9`
- Ollama: `github.com/cloudwego/eino-ext/components/model/ollama v0.1.9`

## Normalized fields

All optional numeric fields must preserve the distinction between “unset” and zero. The UI sends nullable values; Go stores pointer fields where zero is a valid explicit value.

| Normalized field | Meaning | Baseline validation |
| --- | --- | --- |
| `maxCompletionTokens` | Total completion ceiling where the adapter supports reasoning-inclusive tokens | integer `> 0` |
| `temperature` | Sampling randomness | provider range, usually `0..2`; Ark/Claude/Gemini use `0..1` |
| `topP` | Nucleus sampling | `0..1` |
| `topK` | Candidate-token limit | integer `> 0` |
| `presencePenalty` | Topic novelty penalty | `-2..2` |
| `frequencyPenalty` | Repetition penalty | `-2..2` |
| `seed` | Best-effort deterministic sampling | integer |
| `stopSequences` | Generation stop strings | non-empty strings; provider-specific count/length limits may narrow later |
| `responseFormat` | `text` or `json_object` in the first release | closed enum; no arbitrary schema/JSON body |
| `reasoningMode` | `off`, `auto`, or `on` | closed enum |
| `reasoningEffort` | Provider-supported effort tier | closed enum from capability descriptor |
| `reasoningBudget` | Token budget for thinking | integer `> 0` and below output-token constraints where required |

`temperature` and `topP` remain independently configurable because SDKs support both, but the descriptor and tooltip warn that users normally tune one, not both.

## Provider capability profiles

| Provider profile | Stable generation fields | Reasoning fields | Mapping and safe fallback |
| --- | --- | --- | --- |
| OpenAI official / recognized OpenAI reasoning models | max completion tokens, temperature, top-p, stop, presence/frequency penalty, seed, response format | effort: low/medium/high | Agent maps to `ReasoningEffort`; direct HTTP maps `reasoning_effort`. Unrecognized generic gateways do not receive reasoning fields. |
| Generic OpenAI-compatible | max tokens, temperature, top-p, stop, presence/frequency penalty, seed, response format | none by default | Use the conservative common subset. Do not send `thinking` or vendor reasoning objects merely because the endpoint is OpenAI-shaped. |
| OpenRouter | max completion/output tokens, temperature, top-p, stop, presence/frequency penalty, seed, response format | mode, effort none/minimal/low/medium/high, max-token budget | Map to `Reasoning{Enabled, Effort, MaxTokens}`. Session thinking off overrides the saved descriptor without persisting it. |
| Ark | max completion/output tokens, temperature, top-p, stop, presence/frequency penalty, response format | mode off/auto/on, effort minimal/low/medium/high | Map to Ark `Thinking` and `ReasoningEffort`; only emit values returned by the descriptor. |
| Claude | max output tokens, temperature, top-p, top-k, stop | mode off/auto/on, explicit budget; adaptive thinking | Map enabled budget or adaptive union in Agent. Temperature is `0..1`; budget must respect provider/output constraints. |
| Gemini | max output tokens, temperature, top-p, top-k, JSON response mode | mode off/auto/on, level minimal/low/medium/high, budget | Map to `genai.ThinkingConfig`. Capability rules decide whether level and budget may coexist for the selected model family. |
| DeepSeek | max tokens, temperature, top-p, stop, presence/frequency penalty, JSON response mode | stable mode switch only | Map thinking through the DeepSeek adapter when the selected model supports it. Do not expose effort or budget until verified for the pinned adapter/API. |
| Qwen / DashScope | max tokens, temperature, top-p, stop, presence/frequency penalty, seed, JSON response mode | stable mode switch only | Map to `EnableThinking`; omit effort and budget. |
| Ollama | max tokens, temperature and adapter-supported sampling options | stable mode switch only | Map to Ollama `Thinking`; omit effort/budget unless a later adapter contract adds stable support. |

## Capability descriptor rules

- The backend is the single source of truth and returns a descriptor for `{baseUrl, modelName}`.
- Provider detection reuses one shared resolver; request factories and the UI capability API must not maintain independent provider switches.
- Model-name heuristics may narrow a provider profile, but unknown names always fall back conservatively.
- The descriptor contains field visibility, nullable/default semantics, numeric ranges, enum choices, tooltip text, compatibility notes, and conflicts.
- When an existing configuration contains a value no longer supported by the current descriptor, the descriptor reports it as incompatible. The UI warns before save; backend validation prevents unsupported transmission.

## Request-path coverage

The normalized effective configuration must be consumed by both existing request families:

1. Direct streaming/tool requests in `backend/data/openai_tools.go`.
2. Eino Agent model creation in `backend/agent/chat_model_factory.go`.

The per-session thinking switch is an override layered on the saved configuration:

- session off: effective reasoning mode becomes off;
- session on: preserve saved mode, effort, and budget;
- no persistence mutation and no in-place mutation of the cached `AIConfig` pointer.

## Deferred fields

The first release does not expose arbitrary request JSON, logit bias, log probabilities, provider routing, service tiers, safety settings, tool execution toggles, headers, user metadata, JSON Schema editors, or multimodal/image controls. They are either unstable across adapters, difficult to validate safely, or outside the requested model-generation configuration scope.
