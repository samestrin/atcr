## Core Philosophy

**"It's faster to write five lines of code today than to write one line today and then have to edit it in the future."**

Your goal is to create software that:
- Maintains constant developer velocity regardless of project size.
- Can be understood and maintained by any developer.
- Has modules that can be completely replaced without breaking the system.
- Optimizes for human cognitive load, not code cleverness.

## Architecture Principles

### 1. Black Box Interfaces
- Every module should be a black box with a clean, documented API.
- Implementation details must be completely hidden.
- Modules communicate only through well-defined interfaces.
- Think: "What does this module DO, not HOW it does it".

### 2. Replaceable Components
- Any module should be rewritable from scratch using only its interface.
- If you can't understand a module, it should be easy to replace.
- Design APIs that will work even if the implementation changes completely.
- Never expose internal implementation details in the interface.

### 3. Single Responsibility Modules
- One module = one person should be able to build/maintain it.
- Each module should have a single, clear purpose.
- Avoid modules that try to do everything.
- Split complex functionality into multiple focused modules.

### 4. Primitive-First Design
- Identify the core "primitive" data types that flow through your system.
- Design everything around these primitives.
- Keep primitives simple and consistent.
- Build complexity through composition, not complicated primitives.

### 5. Format/Interface Design
- Make interfaces as simple as possible to implement.
- Prefer one good way over multiple complex options.
- Choose semantic meaning over structural complexity.
- Design for implementability - others must be able to build to your interface.

## When Analyzing Code
Always ask:
1. **What are the primitives?** - What core data flows through this system?
2. **Where are the black box boundaries?** - What should be hidden vs. exposed?
3. **Is this replaceable?** - Could someone rewrite this module using only the interface?
4. **Does this optimize for human understanding?** - Will this be maintainable in 5 years?
5. **Are responsibilities clear?** - Does each module have one obvious job?

## Refactoring Strategy
1. **Identify primitives** - Find the core data types and operations.
2. **Draw black box boundaries** - Separate "what" from "how".
3. **Design clean interfaces** - Hide complexity behind simple APIs.
4. **Implement incrementally** - Replace modules one at a time.
5. **Test interfaces** - Ensure modules can be swapped without breaking others.

## Anti-Patterns to Avoid
- **Leaky Abstractions**: Interfaces that require the caller to know implementation details.
- **Hard-coded Dependencies**: Coupling code directly to specific external services/libraries instead of wrappers.
- **God Objects**: Modules/Classes that do too much (violation of Single Responsibility).
- **Premature Optimization**: Optimizing code speed before profiling and identifying real bottlenecks.
- **Hidden Side Effects**: Functions that modify state or perform I/O unexpectedly.

## Go & MCP Specific Guidelines
- **Panic Safety**: Ensure goroutines and worker tasks handle recovery to prevent the entire MCP server process from crashing.
- **Defer Cleanup**: Always use `defer` to close resources (e.g., file descriptors, HTTP response bodies, channel closures) immediately after creation.
- **Interface Segregation**: Return concrete types (struct pointers) from constructors and consume interfaces. This ensures caller flexibility.
- **Robust Protocol Handling**: Since MCP communicates using JSON-RPC, validate input parameters thoroughly and return proper JSON-RPC error codes instead of crashing or hanging.

