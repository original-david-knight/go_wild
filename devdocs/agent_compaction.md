# **Comprehensive Architectural Frameworks for Context Compaction and Memory Management in Go-Based Agentic Systems**

The transition from stateless large language model interactions to autonomous agentic workflows necessitates a fundamental re-evaluation of how state and information are persisted across iterative loops. In the development of agents using the Go programming language—a language favored for its concurrency primitives and performance in production environments—the primary bottleneck is rarely the model’s raw reasoning capability but rather the efficient curation of the context window. As an agent interacts with its environment via tools and functions, the resulting interaction history, or trajectory, expands at a rate that frequently outpaces the model’s ability to maintain high-fidelity attention. This phenomenon, often termed context rot or context pollution, leads to increased latency, compounding operational costs, and a significant degradation in the agent’s ability to retrieve critical information, ultimately manifesting as perceived forgetfulness or logical inconsistency.

The engineering problem at hand is the optimization of the utility of tokens against the inherent architectural constraints of transformer-based models. Effectively managing these systems requires a perspective that views the context window not as a simple buffer of text, but as a holistic state that yields specific behaviors based on its composition. Context engineering, therefore, is the discipline of curating and maintaining the optimal set of information during inference, ensuring that every token contributes to the desired outcome rather than acting as noise that depletes the model’s finite attention budget.

## **Computational and Mathematical Foundations of Context Pressure**

To understand the necessity of context compaction, one must first address the mathematical complexity of the transformer architecture which underpins modern models like Gemini. The self-attention mechanism requires every token in a given sequence to attend to every other token, resulting in a computational complexity of ![][image1] for a sequence of ![][image2] tokens. This quadratic scaling implies that the "attention budget" of the model is a finite resource; every irrelevant token introduced not only increases the cost and latency of the request but also diminishes the model's ability to focus on mission-critical instructions.

In the formulation of agentic tasks, the interaction is often described as a Partially Observable Markov Decision Process (POMDP). An environment ![][image3] is defined by the tuple ![][image4], representing the state space, action space, observation space, transition function, and reward function respectively. At each step ![][image5], the agent generates an action ![][image6] based on its reasoning and the interaction history ![][image7] and the latest observation ![][image8]. The total cost ![][image9] of a task is the summation of per-step costs ![][image10] associated with encoding this dynamic context: ![][image11]. As ![][image12] grows, ![][image13] becomes unbounded, leaving the developer with the choice to either terminate the task prematurely or truncate the context heuristically, which often destroys the coherence of the agent's long-term plan.1

| Problem Manifestation | Mechanical Cause | Impact on Agent Reliability |
| :---- | :---- | :---- |
| Context Poisoning | Ingestion of incorrect tool outputs or hallucinations | Persistent logical errors in subsequent steps |
| Context Distraction | Irrelevant background noise overwhelming instructions | Failure to adhere to system constraints or personas |
| Context Confusion | High volume of similar but distinct data points | Misidentification of variables, file paths, or IDs |
| Context Clash | Contradictory information across different turns | Behavioral paralysis or repetitive looping |
| Lost in the Middle | Key info buried in the center of a long context | Failure to retrieve relevant facts during reasoning |

Source: 2

The "lost-in-the-middle" effect is particularly detrimental for RAG-heavy agents. Research indicates that while models like Gemini 1.5 Pro can maintain consistent performance up to 2 million tokens, their ability to correctly answer questions based on retrieved documents is significantly higher when the context remains within the 128k token range. Beyond this, even if the model does not fail directly, it may return empty responses or "Information not available" if the reasoning steps consume too much of the internal token budget.6

## **Comparative Analysis of Primary Compaction Strategies**

When an agent's context window reaches critical capacity, developers typically rely on one of three foundational strategies: deterministic truncation, LLM-based summarization, or observation masking. The choice between these strategies involves a delicate trade-off between information fidelity, computational overhead, and the risk of inducing unproductive agent behaviors.

### **Truncation and Deterministic Pruning**

The most straightforward method for managing context is the systematic removal of older tokens once a predefined limit is reached. In a Go-based implementation, this typically involves measuring the token count of a message slice and dropping the earliest entries until the total falls below the threshold. A more sophisticated version of this approach distinguishes between must-have and optional context. The system prompt, current user query, and core tool definitions are treated as immutable, while the conversation history is treated as a sliding window.

While truncation is computationally inexpensive and deterministic, its primary flaw is the "amnesia" effect. When an agent's initial plan or a critical user constraint falls out of the window, the agent may begin to hallucinate new goals or ignore established parameters. This is especially problematic in long-running sessions where the agent's "yesterday's plan" might be discarded, leading to a loss of continuity that appears to the user as a failure of intelligence.7

### **The Limitations of LLM-Based Summarization**

Summarization is often viewed as a more intelligent alternative to truncation. This technique uses an LLM to condense a block of dialogue or tool outputs into a concise paragraph that preserves key facts and decisions. However, research into software engineering agents has identified a significant side effect known as "trajectory elongation." When an LLM summarizes a failed tool execution or a complex error log, it tends to "smooth over" the specifics of the failure. The resulting summary may indicate that a task was attempted but failed, without preserving the exact error message that the agent needs to debug the issue.

Consequently, the agent, reading the summary instead of the raw output, may fail to realize how stuck it is and keep repeating the same unproductive actions. Furthermore, summarization calls themselves add to the latency and cost of each turn, sometimes accounting for up to 7% of the total inference budget. The lossy nature of summaries means that exact quotes, specific numbers, and nuanced constraints often vanish, replaced by generalizations that lack the resolution required for complex reasoning.10

### **The Efficacy of Observation Masking**

Observation masking represents a simple yet highly effective alternative that has gained traction in state-of-the-art systems like SWE-agent. Instead of summarizing the entire history, masking focuses specifically on tool outputs—which often constitute over 80% of an agent's token usage. Older observations are replaced with a placeholder, such as \`\`, while the agent's internal reasoning and specific commands are preserved in full.

Empirical studies comparing these strategies on models like Gemini 2.5 Flash and Qwen3-Coder demonstrate that observation masking halves the cost of the raw agent while matching or exceeding the solve rate of LLM-based summarization. By keeping the reasoning trace intact, the agent maintains a clear record of its own thought process and actions, while the omission of raw data prevents context bloat. This challenges the assumption that complex semantic summarization is necessary to preserve critical information. For many real-world workflows, the most recent context is sufficient for the agent to proceed, provided the logical trajectory remains visible.11

| Strategy | Token Cost Reduction | Solve Rate Impact | Implementation Complexity | Primary Risk |
| :---- | :---- | :---- | :---- | :---- |
| Raw Agent | 0% | Baseline | Low | Context Overflow |
| Truncation | \~50% | Variable (Amnesia) | Low | Abrupt Forgetfulness |
| LLM-Summary | \~55% | Competitive | High | Trajectory Elongation |
| Obs. Masking | \~52% | Competitive/Superior | Medium | Loss of raw data |
| Hybrid | \~60%+ | High | Very High | Engineering Overhead |

Source: 11

## **The Virtual Context Management Paradigm: MemGPT and Letta**

A more radical approach to the memory problem is inspired by traditional computer operating systems. MemGPT (MemoryGPT) introduces the concept of "Virtual Context Management," which provides the illusion of an unlimited context window by paging information between a "Main Context" (analogous to RAM) and an "External Context" (analogous to disk storage). This hierarchical architecture allows the LLM to act as its own memory manager, using designated tools to move data between tiers based on its current goals.

### **Memory Tiering and Self-Editing**

The MemGPT architecture divides the agent's memory into specialized sections. The "Core Memory" resides within the main context window and contains highly relevant information that is always visible to the agent. This is typically split into a "Persona" block, defining the agent's behavior, and a "Human" block, containing facts about the user. Because the agent has self-editing capabilities, it can update these blocks dynamically using tools like core\_memory\_append and core\_memory\_replace.17

Information that does not need to be constantly visible is stored in "External Context," which consists of:

* **Archival Memory:** A semantically searchable database (often a vector store) used for long-term knowledge and large document sets.  
* **Recall Storage:** A chronological archive of all past messages and events that can be retrieved via keyword or full-text search.

The innovation of MemGPT lies in the "Heartbeat" mechanism. When an agent calls a memory tool, it can request a heartbeat to continue its execution loop, allowing it to "think" about the retrieved information before responding to the user. This multi-step reasoning allows the agent to iteratively search its own memory until it finds the necessary context to complete a task. This shift from passive retrieval to active memory management enables agents to maintain a coherent personality and deep contextual awareness over months of interaction.17

### **Semantic Memory and Cognitive Triage**

The transformation of episodic memories (specific experiences) into semantic memories (general knowledge) is a core goal of the MemGPT paradigm. Through a process analogous to "semantization" in human cognition, the agent learns to decouple core facts from their specific conversational context. For example, if a user repeatedly mentions a preference for Python, the agent eventually graduates this from a series of chat logs to a stable fact in its "Human" core memory block. This reduces "context pollution" and ensures that the agent's limited attention is focused on the most relevant, high-precision signals.20

## **Advanced Optimization: Gemini Context Caching for Go Agents**

For agents built specifically on the Gemini framework, "Context Caching" offers a powerful tool for reducing both the financial cost and the latency of high-context requests. This feature is particularly effective for agents that possess extensive system instructions, complex tool definitions, or those that frequently reference the same massive document sets.

### **Implicit vs. Explicit Caching Mechanisms**

The Gemini API provides two distinct caching paths. "Implicit Caching" is enabled by default on Gemini 2.5 models. The system automatically caches the tokens of repeated content and applies a cost saving (often up to 90%) when a cache hit occurs. To optimize for implicit hits, developers should structure prompts so that large, common contents—such as tool schemas and system instructions—appear at the very beginning of the prompt. Sending requests with a similar prefix in quick succession also increases the hit probability.21

"Explicit Caching" provides the developer with direct control over the cache lifecycle. By creating a CachedContent object via the API, a set of tokens is manually cached and assigned a Time-To-Live (TTL). Subsequent requests can then reference this cache by name, ensuring a guaranteed discount on input tokens. This is ideal for chatbots with 50+ tools or agents analyzing the same 100MB PDF across multiple turns. In the context of a Go-based agentic loop, explicit caching allows for a "stateless" architecture where the expensive prefix is stored on Google's infrastructure, while the agent only sends the new, unique turn tokens.21

### **Implementation in the Go Ecosystem**

Implementing context caching in Go involves the google.golang.org/genai library. A critical constraint when using cached content is that certain properties—namely system\_instructions, tools, and tool\_config—cannot be redefined in the GenerateContent request if they were already included in the cache. Redefining them will result in an error or the cache being ignored.

Go

// Example of creating a context cache in Go  
func setupAgentCache(ctx context.Context, client \*genai.Client, model string, tools\*genai.Tool) (string, error) {  
    config := \&genai.CreateCachedContentConfig{  
        Model: model,  
        SystemInstruction: genai.Text("You are an autonomous agent..."),  
        Tools: tools,  
        TTL: "3600s", // 1 hour TTL  
    }  
    cache, err := client.Caches.Create(ctx, model, config)  
    if err\!= nil {  
        return "", err  
    }  
    return cache.Name, nil  
}

26

By utilizing the UsageMetadata returned in each response, developers can track the cachedContentTokenCount to monitor the effectiveness of their caching strategy and ensure they are staying within the minimum token thresholds (1024 for Gemini 2.5 Flash, 4096 for Gemini 2.5 Pro).23

## **Architectural Resilience and Memory Persistence in Go**

Go's reputation for building scalable backend services makes it an ideal choice for persistent agentic systems. Several frameworks have emerged that provide clean abstractions for managing the memory hierarchy and agent state.

### **The Lattice Framework and Persistent State**

Lattice is a prominent Go framework built for production AI agents. It addresses the memory problem through a "Memory Engine" that supports multiple backends, including PostgreSQL with pgvector, MongoDB, and Qdrant. One of its key features is "Importance Scoring," which automatically weights memories by relevance and uses "Maximal Marginal Relevance" (MMR) retrieval to ensure that retrieved context is diverse and doesn't trap the agent in a narrow context loop.29

Lattice also introduces the "Agent-as-Tool" architecture. By wrapping an agent as a tool, developers can create hierarchical structures (e.g., a "Manager" agent delegating to a "Coder" agent). This provides "Context Isolation": the sub-agent runs in its own session, and its internal reasoning and verbose tool outputs never pollute the parent's context window. Only the final result is returned, keeping the parent's history clean and focused on high-level coordination.29

### **trpc-agent-go and Graph-Based Memory**

Another advanced framework, trpc-agent-go, offers hierarchical planners and multi-agent orchestration. It utilizes "Graph Agents" to manage complex, conditional workflows. By representing the agent's state as a graph, the system can implement conditional routing and parallel execution of tasks, further optimizing the context by only providing the agent with the "sub-graph" of state relevant to its current node.31

### **Session Management and Checkpointing**

To prevent amnesia during long-running tasks, Go agents should implement "Checkpointing." Frameworks like Lattice provide agent.Checkpoint() and agent.Restore() methods, which serialize the entire agent state—including the system prompt, short-term memory, and shared space memberships—into a byte array. This allows the system to persist an agent's "consciousness" to a database and rehydrate it in a new goroutine or on a different server, ensuring continuity even if the underlying process is interrupted.29

## **Mitigating Forgetfulness through Context Engineering**

The user's experience of an agent being "forgetful" after compaction is rarely due to a lack of total memory but rather a failure of "Signal Retention." When context is compressed, the model often loses "needles"—specific, low-frequency but high-importance facts like an API key, a unique ID, or a subtle user preference.

### **Entity Tracking and Fact Extraction**

A high-fidelity compaction strategy involves a pre-processing step known as "Fact Extraction." Before a block of history is summarized or masked, a secondary LLM call (or a specialized tool) identifies and extracts "Entities" (names, locations, product references) and "Facts" (decisions made, unresolved issues). These facts are then stored separately from the chronological history.

This is the "Hybrid Memory" approach: the agent sees a rolling summary for conversational flow, a sliding window of the last 3 turns for immediate recency, and a persistent "Knowledge Block" containing all the extracted facts. This ensures that even if the raw messages where a decision was made are pruned, the decision itself remains anchored in the agent's core memory.12

### **Cognitive Triage and Aggressive Prompting**

The "Focus Agent" research provides a roadmap for "Intra-Trajectory Compression." The agent is given explicit instructions to "ALWAYS call complete\_focus after 10-15 tool calls." This forces the agent to periodically pause its exploration, consolidate its findings into a "Knowledge Block," and then prune the raw tool outputs. Experiments show that this aggressive, self-directed pruning reduces token usage by 22.7% without hurting accuracy. The key is to treat attention as a finite budget and empower the agent to decide when the "noise" of its previous exploration is no longer needed.5

| Memory Type | Storage Location | Retrieval Mechanism | Best For |
| :---- | :---- | :---- | :---- |
| Working Memory | Context Window | Immediate Attention | Immediate tasks, tone |
| Episodic Memory | Vector DB | Semantic Search | Recalling "What happened" |
| Semantic Memory | Core Memory Block | Hard-coded/Fixed | "What is true" (Facts) |
| Procedural Memory | System Prompt | Hard-coded/Fixed | "How to behave" (Instructions) |

Source: 3

## **Strategic Design Principles for Agent Scaffolding**

To move beyond "dumb" truncation, the following design patterns should be integrated into the Go agent's control loop. These patterns focus on forcing structure on the agent's reasoning process and offloading state to the environment wherever possible.

### **Structured Note-Taking and External Verification**

One of the most effective ways to manage context is to stop relying on the conversation history as the primary source of truth. Instead, agents should be prompted to maintain an external progress.txt or NOTES.md file. This file acts as a "Scratchpad" that persists outside the context window. The agent can read and update this file as needed.

By forcing the agent to document its work and track progress explicitly, the system prevents the "Premature Victory" problem, where an agent assumes a task is done because it has "forgotten" the missing subtasks that were mentioned 50 turns ago. Furthermore, the agent should be required to verify its work through real tests or environment observations rather than self-assessment. External verification ensures that even if the agent's internal "memory" of the task is hazy, the state of the environment provides an objective signal of what remains to be done.3

### **Recursive Task Decomposition (ReCAP)**

The "Recursive Context-Aware Reasoning and Planning" (ReCAP) framework frames the agent's execution as a tree. Each recursive call extends the context with local reasoning traces for a specific subtask. When the subtask is complete, the agent "backtracks," returning control to the parent node and discarding the local reasoning traces, only bringing back the summary result. This prevents the parent's context from being polluted by the "trial and error" that occurred during subtask execution.40

## **Synthesis and Comprehensive Recommendations**

For a Go-based agent utilizing Gemini with extensive tool usage, the most effective context management strategy is not a single algorithm but a "Hybrid Cognitive Architecture." The following multi-layered approach is recommended to solve the problem of rapid context bloat while eliminating the forgetfulness associated with standard compaction.

### **The Recommended Architecture: The "Cognitive OS" Pattern**

1. **Foundational Caching (Gemini Explicit Cache):**  
   The system must first implement explicit context caching for the immutable portions of the prompt. This includes the agent's persona, the schemas for all functions and tools, and any baseline documentation. By caching these tokens with a 1-hour rolling TTL, the developer achieves significant cost reductions and minimizes the "attention load" on the model for every turn.  
2. **Turn-Based Observation Masking:**  
   The agent should implement a sliding window for tool observations. The last 3 tool outputs (observations) should be kept in full. Any observations older than that should be replaced with a one-line placeholder summary (e.g., "Output of read\_file('main.go') was a 500-line source file; agent extracted logic for user authentication"). This preserves the reasoning trajectory—the most critical part of the history—while discarding the verbose data.  
3. **The Persistent "Core Memory" Block:**  
   The system should maintain a dedicated struct in the Go application that is rendered into the system prompt for every turn. This block should contain:  
   * **The Current Plan:** A list of subtasks and their status (Pending, In-Progress, Done).  
   * **Extracted Knowledge:** A list of key facts, identifiers (UUIDs, API keys found), and user preferences.  
   * **Session Summary:** A 2-3 sentence overview of the conversation's progress.  
4. **Autonomous Memory Management Tools:**  
   The agent must be given tools to manage its own state. Specifically:  
   * update\_plan(subtask, status): Updates the planning block.  
   * save\_fact(fact\_description): Adds a new fact to the knowledge block.  
   * search\_history(query): Performs a semantic search against the full, unmasked history stored in a vector database (e.g., pgvector).

### **Why this Recommendation Solves the User's Problem**

The user's current compaction code likely treats all history as equally valuable and then "blindly" summarizes it, leading to the loss of specific details. The "Cognitive OS" pattern solves this by:

* **Decoupling Reasoning from Data:** Masking keeps the "why" and "how" while discarding the "what."  
* **Anchoring through Core Memory:** The most important facts are moved out of the "unreliable" chronological history and into a "reliable" fixed block.  
* **Offloading through Caching:** High-volume static context (tool docs) is handled at the infrastructure level, freeing up the model's focus for the current turn.

By implementing this hierarchy, the agent transitions from a stateless loop to a stateful service that intelligently manages its own attention. This mimics the human professional who uses a notebook (Core Memory), a file system (Archival Memory), and a short-term working memory (Sliding Window) to accomplish complex, long-horizon tasks without drowning in information. The result is an agent that is significantly cheaper to run, faster to respond, and far less likely to "forget" the mission-critical details of its work.

#### **Works cited**

1. Acon: Optimizing Context Compression for Long-horizon LLM Agents \- arXiv, accessed February 3, 2026, [https://arxiv.org/html/2510.00615v1](https://arxiv.org/html/2510.00615v1)  
2. 6 Techniques You Should Know to Manage Context Lengths in LLM Apps \- Reddit, accessed February 3, 2026, [https://www.reddit.com/r/LLMDevs/comments/1mviv2a/6\_techniques\_you\_should\_know\_to\_manage\_context/](https://www.reddit.com/r/LLMDevs/comments/1mviv2a/6_techniques_you_should_know_to_manage_context/)  
3. Context Engineering \- LangChain Blog, accessed February 3, 2026, [https://www.blog.langchain.com/context-engineering-for-agents/](https://www.blog.langchain.com/context-engineering-for-agents/)  
4. Memory for AI Agents: A New Paradigm of Context Engineering \- The New Stack, accessed February 3, 2026, [https://thenewstack.io/memory-for-ai-agents-a-new-paradigm-of-context-engineering/](https://thenewstack.io/memory-for-ai-agents-a-new-paradigm-of-context-engineering/)  
5. Context Engineering: The Real Reason AI Agents Fail in Production \- Inkeep, accessed February 3, 2026, [https://inkeep.com/blog/context-engineering-why-agents-fail](https://inkeep.com/blog/context-engineering-why-agents-fail)  
6. The Long Context RAG Capabilities of OpenAI o1 and Google Gemini | Databricks Blog, accessed February 3, 2026, [https://www.databricks.com/blog/long-context-rag-capabilities-openai-o1-and-google-gemini](https://www.databricks.com/blog/long-context-rag-capabilities-openai-o1-and-google-gemini)  
7. Top techniques to Manage Context Lengths in LLMs \- Agenta, accessed February 3, 2026, [https://agenta.ai/blog/top-6-techniques-to-manage-context-length-in-llms](https://agenta.ai/blog/top-6-techniques-to-manage-context-length-in-llms)  
8. Memory Optimization Strategies in AI Agents | by Nirdiamant \- Medium, accessed February 3, 2026, [https://medium.com/@nirdiamant21/memory-optimization-strategies-in-ai-agents-1f75f8180d54](https://medium.com/@nirdiamant21/memory-optimization-strategies-in-ai-agents-1f75f8180d54)  
9. Context Engineering \- Short-Term Memory Management with Sessions from OpenAI Agents SDK, accessed February 3, 2026, [https://developers.openai.com/cookbook/examples/agents\_sdk/session\_memory/](https://developers.openai.com/cookbook/examples/agents_sdk/session_memory/)  
10. Context Length Management in LLM Applications by cbarkinozer | Softtech \- Medium, accessed February 3, 2026, [https://medium.com/softtechas/context-length-management-in-llm-applications-89bfc210489f](https://medium.com/softtechas/context-length-management-in-llm-applications-89bfc210489f)  
11. Cutting Through the Noise: Smarter Context Management for LLM ..., accessed February 3, 2026, [https://blog.jetbrains.com/research/2025/12/efficient-context-management/](https://blog.jetbrains.com/research/2025/12/efficient-context-management/)  
12. Never Forget a Thing: Building AI Agents with Hybrid Memory Using ..., accessed February 3, 2026, [https://dev.to/aws/never-forget-a-thing-building-ai-agents-with-hybrid-memory-using-strands-agents-2g66](https://dev.to/aws/never-forget-a-thing-building-ai-agents-with-hybrid-memory-using-strands-agents-2g66)  
13. Agent Context Management: Why Simple Observation Masking Beats LLM Summarisation | by balaji bal | Medium, accessed February 3, 2026, [https://medium.com/@balajibal/agent-context-management-why-simple-observation-masking-beats-llm-summarisation-4961cb67be89](https://medium.com/@balajibal/agent-context-management-why-simple-observation-masking-beats-llm-summarisation-4961cb67be89)  
14. The Complexity Trap: Simple Observation Masking Is as Efficient as LLM Summarization for Agent Context Management \- arXiv, accessed February 3, 2026, [https://arxiv.org/html/2508.21433v3](https://arxiv.org/html/2508.21433v3)  
15. The Complexity Trap: Simple Observation Masking Is as Efficient as LLM Summarization for Agent Context Management \- arXiv, accessed February 3, 2026, [https://arxiv.org/html/2508.21433v1](https://arxiv.org/html/2508.21433v1)  
16. The Complexity Trap: Simple Observation Masking Is as Efficient as LLM Summarization for Agent Context Management | OpenReview, accessed February 3, 2026, [https://openreview.net/forum?id=OHVzruJl5k](https://openreview.net/forum?id=OHVzruJl5k)  
17. LLMs as Operating Systems: Agent Memory \- DeepLearning.AI, accessed February 3, 2026, [https://learn.deeplearning.ai/courses/llms-as-operating-systems-agent-memory/lesson/wimxl/understanding-memgpt](https://learn.deeplearning.ai/courses/llms-as-operating-systems-agent-memory/lesson/wimxl/understanding-memgpt)  
18. Virtual context management with MemGPT and Letta \- Leonie Monigatti, accessed February 3, 2026, [https://www.leoniemonigatti.com/blog/memgpt.html](https://www.leoniemonigatti.com/blog/memgpt.html)  
19. MemGPT | Letta Docs, accessed February 3, 2026, [https://docs.letta.com/concepts/memgpt/](https://docs.letta.com/concepts/memgpt/)  
20. MemGPT: Engineering Semantic Memory through Adaptive Retention and Context Summarization \- Information Matters, accessed February 3, 2026, [https://informationmatters.org/2025/10/memgpt-engineering-semantic-memory-through-adaptive-retention-and-context-summarization/](https://informationmatters.org/2025/10/memgpt-engineering-semantic-memory-through-adaptive-retention-and-context-summarization/)  
21. Vertex AI context caching | Google Cloud Blog, accessed February 3, 2026, [https://cloud.google.com/blog/products/ai-machine-learning/vertex-ai-context-caching](https://cloud.google.com/blog/products/ai-machine-learning/vertex-ai-context-caching)  
22. Deep Dive into Gemini Context Caching: Best Practices & Trends \- Sparkco, accessed February 3, 2026, [https://sparkco.ai/blog/deep-dive-into-gemini-context-caching-best-practices-trends](https://sparkco.ai/blog/deep-dive-into-gemini-context-caching-best-practices-trends)  
23. Context caching overview | Generative AI on Vertex AI \- Google Cloud Documentation, accessed February 3, 2026, [https://docs.cloud.google.com/vertex-ai/generative-ai/docs/context-cache/context-cache-overview](https://docs.cloud.google.com/vertex-ai/generative-ai/docs/context-cache/context-cache-overview)  
24. Context caching | Gemini API | Google AI for Developers, accessed February 3, 2026, [https://ai.google.dev/gemini-api/docs/caching/](https://ai.google.dev/gemini-api/docs/caching/)  
25. Context caching | Gemini API | Google AI for Developers, accessed February 3, 2026, [https://ai.google.dev/gemini-api/docs/caching](https://ai.google.dev/gemini-api/docs/caching)  
26. Caching | Gemini API \- Google AI for Developers, accessed February 3, 2026, [https://ai.google.dev/api/caching](https://ai.google.dev/api/caching)  
27. Create a context cache | Generative AI on Vertex AI \- Google Cloud Documentation, accessed February 3, 2026, [https://docs.cloud.google.com/vertex-ai/generative-ai/docs/context-cache/context-cache-create](https://docs.cloud.google.com/vertex-ai/generative-ai/docs/context-cache/context-cache-create)  
28. Use a context cache | Generative AI on Vertex AI \- Google Cloud Documentation, accessed February 3, 2026, [https://docs.cloud.google.com/vertex-ai/generative-ai/docs/context-cache/context-cache-use](https://docs.cloud.google.com/vertex-ai/generative-ai/docs/context-cache/context-cache-use)  
29. Protocol-Lattice/go-agent: An agent framework for Go with graph-aware memory, UTCP-native tools, and multi-agent orchestration. Built for production. \- GitHub, accessed February 3, 2026, [https://github.com/Protocol-Lattice/go-agent](https://github.com/Protocol-Lattice/go-agent)  
30. Lattice Agent \- AI agent framework with memory layer in Golang : r/vibecoding \- Reddit, accessed February 3, 2026, [https://www.reddit.com/r/vibecoding/comments/1ofrpmw/lattice\_agent\_ai\_agent\_framework\_with\_memory/](https://www.reddit.com/r/vibecoding/comments/1ofrpmw/lattice_agent_ai_agent_framework_with_memory/)  
31. trpc-group/trpc-agent-go \- GitHub, accessed February 3, 2026, [https://github.com/trpc-group/trpc-agent-go](https://github.com/trpc-group/trpc-agent-go)  
32. Is “Summarized Context \+ Sliding Window” the Best Memory Strategy for Azure OpenAI Agents? \- Microsoft Learn, accessed February 3, 2026, [https://learn.microsoft.com/en-gb/answers/questions/2259997/is-summarized-context-sliding-window-the-best-memo](https://learn.microsoft.com/en-gb/answers/questions/2259997/is-summarized-context-sliding-window-the-best-memo)  
33. Simplifying RAG Context Windows — How to Stop Your Agent Forgetting | by Levi Stringer, accessed February 3, 2026, [https://medium.com/@levi\_stringer/simplifying-rag-context-windows-with-conversation-buffers-how-to-stop-your-agent-forgetting-df2149ad7403](https://medium.com/@levi_stringer/simplifying-rag-context-windows-with-conversation-buffers-how-to-stop-your-agent-forgetting-df2149ad7403)  
34. Active Context Compression: Autonomous Memory Management in LLM Agents \- arXiv, accessed February 3, 2026, [https://arxiv.org/html/2601.07190v1](https://arxiv.org/html/2601.07190v1)  
35. Agent Loop \- Strands Agents, accessed February 3, 2026, [https://strandsagents.com/latest/documentation/docs/user-guide/concepts/agents/agent-loop/](https://strandsagents.com/latest/documentation/docs/user-guide/concepts/agents/agent-loop/)  
36. Effective harnesses for long-running agents \- Anthropic, accessed February 3, 2026, [https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents)  
37. The Discipline Layer: Harnesses as the Missing Piece in Autonomous Coding \- Maxim AI, accessed February 3, 2026, [https://www.getmaxim.ai/blog/the-discipline-layer-harnesses-as-the-missing-piece-in-autonomous-coding/](https://www.getmaxim.ai/blog/the-discipline-layer-harnesses-as-the-missing-piece-in-autonomous-coding/)  
38. Context Management for Deep Agents \- LangChain Blog, accessed February 3, 2026, [https://www.blog.langchain.com/context-management-for-deepagents/](https://www.blog.langchain.com/context-management-for-deepagents/)  
39. Best practices for coding with agents \- Cursor, accessed February 3, 2026, [https://cursor.com/blog/agent-best-practices](https://cursor.com/blog/agent-best-practices)  
40. ReCAP: Recursive Context-Aware Reasoning and Planning for Large Language Model Agents \- arXiv, accessed February 3, 2026, [https://arxiv.org/html/2510.23822v1](https://arxiv.org/html/2510.23822v1)

[image1]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAADIAAAAYCAYAAAC4CK7hAAACDklEQVR4Xu2Wvy8FQRDHJ37/riQiQSHxI0gkiIhCodNriI5WIfEPqFAgOhWFgtCoKBQv8SM6LSoqkUgkChGFMPN2N+bNzZ59551E4pN8k93vzM7t3t3uHcA/qVCMekDtyMBfYgFVy/ofqHLW/3WOUPXSDOAdzOQd1N5i/RNUJ+sH0466AVOQ9IyaysmIsodqkGZC6JrTwttHVQovllUwhcaYt2y9VuZxeiH3jv6ECvDX8vkRKPFYmhZ3gVEZAOM3SzMBNRA/2VuIj2dxr1EcvhzNS8Iba9NTltCrFXst3wQlWt4ZalF4SXhl7XHUBOtz6Pr05FS0CWpoedRvEh5RhrqDr/w61BOYhZPXY33i2npc/DjmZMBz407BDJyXAQXfQjSc78bwPaTVCYVOT3VsaNEB0HNln6hC3ds2xQ9YzHnauBBGwDM2tKjLK2JetfV8+I5l8nyn43fQB1erGbSQQTA5GRmA+LHboMfJa5NmIH2g14RH8AQYcYv1+YQ27oJ5S+Df1D5mIFozSyOYwLoMWM7BxOkU0lCLWij2onhzrJ0vG2D+6VRoM1LRSea502GTeRq7qDVpWmj8kOLRh42eBv0t5AuNb5Emh95Z9yo4hdANuR8zDn0fJCVgfkJnZSCQ0Hklgop3STMFriDlhVxCdC+kAS1iWJqF5hBVKs0CsoLqkGZapPXY+yG92v8UnE95AJUfn/yG0gAAAABJRU5ErkJggg==>

[image2]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAwAAAAXCAYAAAA/ZK6/AAAAh0lEQVR4XmNgGAWDDXwG4iYk/kog/gDEakhicPAfiIWhtAQQ/4SKc0DF9KB8MCgD4nQgZoFK9iFLQsV2IAs8h9JFDBBJdAASq0MXBAGQBLoGayxicACS+I5FDMU5yAAkWYtFjAfK/ossIcSAaXU8kpgfsgQImDBAQgsdBAPxWSDmRpcYBfgAALQwHoGNEhrEAAAAAElFTkSuQmCC>

[image3]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAwAAAAXCAYAAAA/ZK6/AAAAiklEQVR4XmNgGL7gDBD/R8N1KCqQAEhyFRK/CCqWiCQGByCJ02hiO4B4KZoYGAgyQDSsR5fAB2DunY8ugQv0M2B6Fi9oB+JXQOyOLoENPAHiB+iCuAAnAxHWIwOQ4iR0QVwgmgGiwRFdAgpAcrLIAmZQQRC2QBIHeRynMy8yYAbnZyBmQ1Y0CogFAFD8JuTTK+DSAAAAAElFTkSuQmCC>

[image4]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAH4AAAAXCAYAAADN2PsaAAADpUlEQVR4Xu2ZS8hOQRjHH0Xucs9GKEmuKSWXxMrCLTtKkkKkiA1ZKYnEwgIRiigr14VQRFbKSi4LfLmT5JKExPyb9/jm/Z+ZOTPzDvrq/OrpO/N/nvnPnMv7vTPnFampqamp6dgMZSGUXSz8R36pOMZiB+VfnccjFkLBxfbxQHQNxyKzKAOF73FORDJatM90FSsbxxwpnJSyjy966W7BzJSyB8e+P9XNzGChig0qDrFogMHukHa7od8kvRU2SvvJtXLj14j2KMBxf2ojxhpaCOjzWsUYFQNJN8dLBR53VSxtHCNmq9iq4pShIa42+ph8ZqEK36Rxw115l54K/EY0/p6mXChzpX1eI41jk6kSf7Oeir1+i2h9FumxsDfaL0krcM39iYpRLPo4y4KBa5DcjFNxRMUg0eOdb04Hg74DjGPX3H05G67aWB8bL6TsgfY20gqQe8Wi6Af9LYsudrJAPJY8J1eF6Y/j60Y7FJ7nfuOY4dpU4PGFxRaZIu65jRd3DnxgwYXPBHSW9ouEONGczgIervtGG+M8NNqhoN97Fh2g9huLkcwX7bOZEy1yWOz3BQtF6D05YTBZRScWmU2i/72GgMWR+QAsaU4n013Kn5hijBjggz5zOOEAtRNYjCRlniGYvsVCFRH6oatc5KVM+pnkPWGbT4r/etF9pnHCwl6J97eRMs8QCt/dxnHlzTSonJOvALmDLDbIdcLbRfvMokjxx5YHfZaTbiPF30YuHwaexXYW28bYcc6J3gY6OaBiNYtSPVBVPpTCxxUxTBLd5xMnCCycfrCYCMZ7w2KLFFtNk+J67CHdBfe3YiuquvBV+RCw7VjAYoNU/6p+g8Wfj2GhaK95nGiR51KeI9YtrnNbx4JiFQs2bPvlYpA20kGb6O9IG67JAbyQwadjiOga3+rb5oOFDbR7pJsU/RBrKXdR/GPiYtnGdRFTC4r6xZww6Cu6ph8nRO/bkXtnaGj3NtrgArWddBE9oEnxNsoWK4w6xncx8J1l+vjgMQGe7JC+H6XcH7HDLLIwUcL8QQ/RdXi1GsoT0X1uccLgmrjH7yPlc7Lh0q1EFVewjAUD7NVvsBhJzrnaCPFHzWUWAxim4hKLGcH+fzmLPr6q6MZiIr5/xa2Cr4mjLGamjYWMYJc0nMWMhDy0TeBNz08WE7gi9sVGLr6zkJnoCxfJGRYyg19Zo8lx43NvbZiuLGQGP3L8Lbz76gzgV8kk8E6+puOClzY1NWV+A4TnS1T+eZhDAAAAAElFTkSuQmCC>

[image5]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAcAAAAZCAYAAAD9jjQ4AAAAYklEQVR4XmNgGAIgB10ABk4A8X90QRgASWCV5GCASBxElwCBCgaIpAeyoDUQO0AlQBjEBmEUgNM+EABJTEEXhAGcuuIZ8EiCJBaj8VE4Nkj8SiQ2w1MgrgJiUSD+hSwxTAAAmW4XY6PBOWkAAAAASUVORK5CYII=>

[image6]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABAAAAAYCAYAAADzoH0MAAAAtklEQVR4XmNgGAWjgAYgCYj/A/ENIJ4OxBuAmBNFBR4A0vgSib8EKkYUsGeAKGZFEpsJFSMKgBQ+wCKGzQCQmB+ygC5UMAJZECqGywAUEI5NkAEilgFlLwbiv1AxDIPl0AWAQAMqxgzE9UAcBhVXAuI6mCJksIYBEmggcBKI1zNADOAD4t8wRUDwAomNASoZIJrMoPxJQPyZAeIKGEB3KUlAgQFhACiNkAyYGCAGrADiQDS5EQ0ABRAsUF9ofzsAAAAASUVORK5CYII=>

[image7]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAASEAAAAYCAYAAAC1H0vKAAAGJ0lEQVR4Xu2ca8htQxjHH/dIpBCJ9+T2AbkkySFeJOSW+6XcPxAiHeR+wgeRe7klPpBbKQm5plxDRG7hOM7JneQaisT8mxn7Wf/9rDWz9trv3ts+86und83/mTVrnjWzZ82aWeeIFAqFQqFQKBQKhUKhMDa2Z6FQKHRmb2fXsNjE587+CfYc+aaZD2XZirdQGCXbODuExTpmnZ0tfhDav+qaav5moVAoDBWMKfNYrONo8ScsK/zhbDkWC4XCUDlZWowr7zn7jcUpZV1pcWMKhUInsn9ryLgji1NKXP8qFApzz0POrmCRiTODNZ395eyVkN5BZ5oiENu7LBJfic/3dviLezNsvpPegHiTs5+crVzJMRxOEn8NLMT/LnOzGI9XW8TzjfhrrSE+nmnmYfGxvhT+Hll1jx20Ceql2+SZSo7RsJf4caWRN8VXEp01snPQxsnt0vuR5loOyHcGi4H1xftXU9r5QRsWsXPgWpEHgjZs0AG54+E6w1yUt+Jp0x7/RxAbtqEjeHhMUryPS399kH6StFHBdenD6jDHGlpXNnL2GItjAHEdyGIAPp46Ykao78Vnzi5w9qWzg5Sei3W/7zS065w94uw+Z/eQL4cLpb9MYF3/GEq3AWUtMTS+BpiUPtAFtP+PLIqPd1MWBwTtsROLLahrk31J68o7LNRg9YUKyPCyoWGGxCQLM9jC2SXiz8UIPW5Qj81YFD87sOK7Wnr67lLtHFb+FDhnqaHhlUyjf6z3quNcUKZVP62f6mw7lW7Lg2Kfy/FMWh8YlGvFjhevOtAPU5qVLwXa41zx584nXy5Wm+xnaOBwsfUUaEu8ZuWem8yHDKgka9ZInCysgUnpgKjHHiyK1634fpWejoF5JeX7WR3nsLX4so4gHdp6Ko2PvC5X6QXO9lHpHFDmByyKHSenc7HKAhxPZFL6wKDUxXueeF2v6Vn5csG5gw5CVh2fNTTwqbMXWWyBVaZFY749pT8DFi61trZUv6rm/Lm07YBzuSZ0DotSX4bW2f++sxnSmjhK+suIMwQNdhT0uhWm53eodA4okxdLVwn6waTz9XOx7pmOhzs49DZ9YNKw4gVajzPqurw54Lwug9DThqbbJD4Mo10VfG3Jja8xnzVC6grjY6MIRk0Org0ocxI6IOqBdRbG6jSrB22tkGb/W1JdoARWOZEZ6ffhw8moxfWop5ydEo4BpvkvqDRoug6Aj9e+Folfy2LqysECPXxXsiNg1QHxxDUT9iFd1wfgW8qiIv5wdmGHwqqP5iNp9qfiXSL2+dAubki3BefXDUKpOsJ3g6FZbWLF0oac82ckkQ9O7qjQYAvFb1FrfUWVPjFodcbTcWhPkDYO4pYls7x4fZOQ/iKkNZzGtveGpMX464DvlnD8uvgBB9o64n/AAOtQZ4VjgI0CzAw18Tqrkh7BToiuB44fVWlNXX23leZ48GoK3wohjWPE87X4eLYKegT+uj6QiudM8f5L2aFoqitI+VPxAsx08PoF8DCz8rJ2V9DqjIFWN9im6qjbROdFm3wi1TbhBwLXi42xNAZvVreyqImdnsHrwEWk5VywCZyPDjpuTpPmWLClvVjsnY5XxQ9Wke/VsWZzFgjsrn0rvW+xsIj7hvTKPsDZZeEYYEDCQGQxw4ICi87ofKndtab7AVJ+3BfEE8FGB+JhUn3gbmmOZ1Sk4r1f/Hpg3b+1TJ2fAufvyiLRdI0txbfJzSGNzyfQJqf/l8PPtLt+oNxUhwjy6HXUTsSn6A8VNR9Uhr9ZGRe/SHVWlwue0ieodF0jYNrfFQyEEesHDY5jYUDq4ogMIx6Q6gOpeoyKLvHOk95M6XmltwH3YZZFoksdwTDudaqM4yWdpxUfi9+ibAsWtuNiJb6cxPG4wZMBT4pBQBwYjG4Uf08YvJ5Z35G0BdfZTfq/U9LU6bkcKr1tZ7SL1TZ46GzAYkty+8D1LIwBtF+XeDGbxQMbH6C2Be0R7xN2Yuvu0zDa5DXxO65/siMD1Guh+HpiF3e24u0BP7/yFRTY8Wv1Hy9lgtefUYD3+1EQ18jmmlHFk2JU7deFUbVJFzZ2dhuLhX4GeQoUCoU0XWfphUKhUCgUClPMv1pj/Wm+v9qNAAAAAElFTkSuQmCC>

[image8]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAA8AAAAZCAYAAADuWXTMAAAAn0lEQVR4XmNgGAWjgAyQCcTfgXgfEPOgyeEEvUD8H00MxL+FJoYVgBTWYxFDNxAD7GXArug1A3ZxFIDNVhBAtxnELkTiM0RABQWQBYGAFSqOrhkFmGMTBIK/DBBxLiBOA+JaKB9EowBsmkFi8Uh8RiBeisSHA18GSNyCgAYDRKMhQhoMiqByZAFsriMawDS7oIgSCfKB+BcQs6BLjCQAANU7JYod/7X6AAAAAElFTkSuQmCC>

[image9]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAA8AAAAZCAYAAADuWXTMAAAArUlEQVR4XmNgGAXYgD0Q1wKxN5q4AhofDnYC8X8g3gXERlAxXqiYIJTGCkASuCR5GPDIz2eASDigiSMDkPxpdMGnUAlCYDMDxOkoAKdz0MAGdAFQaII0zkKXIAb8ZoBolkeXIAYQ62SsgFjNV4FYBF2QWM1Y1fgwQCTU0SWQwBJ0AWSAz/aVQHwKXRAZZDBANL8CYk+omDAQrwHiTJgiQkAciEOB2A5dYhQMJAAAfngo8pj2+6YAAAAASUVORK5CYII=>

[image10]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAsAAAAYCAYAAAAs7gcTAAAAiElEQVR4XmNgGD5AB4jfAPF/JIwBeBggEnxIYj+hYihABJsgA0TsEDbBW2hibFBxRmTBOKggOgCJFWIT3IQuiA1wMkAUC6JLYANXGLA7ASvAGY7YAFmKmdAloGAJMuc+A0TxbmRBKNjFgMVWmOkgXADEiUB8D4irkBXBADMDqgYQ9kFRMQrQAABUQSinMwkjkwAAAABJRU5ErkJggg==>

[image11]: <data:image/png;base64,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>

[image12]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAA4AAAAYCAYAAADKx8xXAAAAcklEQVR4XmNgGFngPxH4NRCfAWJ9qB4GV6jEZiCOBGIHJMVeUL4LEM+DisEBCgcKYBrRAeUaryCLIgGQgjXogkAwG10AGZQzQDQao0sQAricSRCANH1AFyQE8hkgGo3QJQiBVwwUOJP2GmGKseFRMPgBAAfAMP3XAavTAAAAAElFTkSuQmCC>

[image13]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAsAAAAZCAYAAADnstS2AAAAoUlEQVR4XmNgGPpACYiTgFgXXQIdOABxDRD/B2IhVCnswIkBopgosBuI/6IL4gIgU0GmEwQsDAgn2APxLiA+CMSMcBVIYC0DRPFzJLFlUDEMABJEl+jDIgYG2BS/xiIGBiBBUISgi+FUjA5AYk3ogv1QCWQAsgUmVgrExTCJ30C8BsaBgltA/ArKRjEIxDFBFgCCxUC8HogVgbgATW4UkAYAUa8mWJ76Ol0AAAAASUVORK5CYII=>