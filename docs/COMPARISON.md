# Comparison of `pgdbtemplate` to Other Libraries

## Analysis of [`pgtestdb`](https://github.com/peterldowns/pgtestdb)

*Analysis conducted on September 21, 2025*  
*Updated on October 11, 2025*

This comparison examines `pgtestdb` as a notable competitor
in the PostgreSQL testing space. By analysing competing solutions,
we aim to help developers choose the best tool for their specific needs
and project requirements.

### Comparative Analysis: `pgdbtemplate` vs `pgtestdb`

| Category | `pgdbtemplate` | `pgtestdb` | Winner & Rationale |
| :--- | :--- | :--- | :--- |
| **Architecture & Dependencies** | **Fully driver-agnostic.** Clean interfaces (`ConnectionProvider`, `MigrationRunner`). Users explicitly choose and import only the needed driver (`pgdbtemplate-pgx` or `pgdbtemplate-pq`) and migration adapter if needed. | **Tightly coupled to `database/sql`.** Users must pass driver names (`"pgx"`, `"postgres"`) as strings, requiring imports of both drivers even when using only one. | **pgdbtemplate**. Modern, clean architecture without implicit dependencies. |
| **Flexibility & Control** | **High.** Separation of concerns: dedicated connection provider and migration runner. Full support for custom implementations. Explicit control over pooling and connection settings. | **Low.** Monolithic `Config` and `Migrator`. Connection settings and migration logic are mixed. Less control over pooling and connection lifecycle. | **pgdbtemplate**. Gives developers complete control over all database aspects. |
| **Performance** | **~28.2–28.8ms per DB** (consistent). **+37% faster** on 200 DBs. Detailed benchmarks analysing schema complexity impact. | Claims **"~10ms per clone"**, but lacks detailed public benchmarks and methodology. | **pgdbtemplate**. Performance backed by data and is a key differentiator. |
| **Security & Enterprise** | **Explicit lifecycle management** (`Initialize`, `Cleanup`). Thread-safe. Support for ready-made pooling options (`MaxConns`, `MinConns`, `Lifetime`). | **Magic database creation inside tests.** Automatic cleanup only if tests pass. May be non-deterministic in complex scenarios. | **pgdbtemplate**. Predictability and control are critical in enterprise environments. |
| **Migration Handling** | **Flexible and modular.** Built-in `FileMigrationRunner` for SQL files. Dedicated adapters for popular frameworks: [goose](https://github.com/andrei-polukhin/pgdbtemplate-goose), [golang-migrate](https://github.com/andrei-polukhin/pgdbtemplate-golang-migrate), [Atlas](https://github.com/andrei-polukhin/pgdbtemplate-atlas). Each adapter is a separate module - import only what you need. | **Strong suit.** Wide selection of built-in adapters for popular migration frameworks (`goose`, `golang-migrate`, `atlas`, etc.), but all bundled in one package. | **Tie**. Both support major migration frameworks. `pgdbtemplate` offers better modularity; `pgtestdb` offers more convenience. |
| **User Experience (UX)** | **More verbose,** but more explicit. Requires understanding the architecture. Ideal for long-term and complex projects. | **Simpler to start.** `pgtestdb.New(t, config, migrator)` returns a ready `*sql.DB`. Quick adoption. | **pgtestdb**. Better for rapid prototyping and smaller projects. |
| **Testing Design** | **Dependency injection.** You receive connection and its name to pass to your repository or service. | **Dependency creation inside test.** Library creates and injects `*sql.DB` itself, which may violate application architecture. | **pgdbtemplate**. Promotes clean architecture and DI. |
| **Output Verbosity** | Minimal. Operates quietly. | **Detailed logging.** Logs connection strings for debugging on test failures, which may clutter output. | **Tie.** Depends on preferences: silence vs. debug information. |

### Key Takeaways and Positioning

**`pgdbtemplate`** positions itself as
"the tool for perfectionists and enterprise developers".

**Strengths:**
1. **Architectural purity and control.** No imposed solutions, just powerful tools.
2. **Proven performance.** Not just claims, but benchmark results.
3. **Thread safety and predictability.** Critical for large projects with parallel tests.
4. **Flexibility.** Easily integrates into any existing application architecture
  through dependency injection.
5. **Modular migration support.** Dedicated adapters for popular frameworks as separate,
  optional modules. Import only what you need, keeping dependencies minimal.

**`pgtestdb`** positions itself as
"the tool for quick starts and projects with diverse migrations".

**Strengths:**
1. **Ease of initial setup.** One function call and you're writing tests.
2. **Bundled migration framework support.** All adapters in one package.
3. **Automation.** Self-cleanup after successful tests.

### Choosing the Right Tool

**Choose `pgdbtemplate` if you need:**
- Maximum performance and scalability
- Architectural flexibility and explicit control
- Enterprise-grade predictability and thread safety
- Integration with existing dependency injection patterns
- Modular dependencies - import only the driver and migration adapter you actually use

**Choose `pgtestdb` if you prioritise:**
- Rapid prototyping and quick setup
- All migration adapters bundled in one package (convenience over modularity)
- Minimal configuration for smaller projects

This analysis demonstrates our commitment to transparency and
helping developers make informed decisions. Both libraries serve important niches
in the PostgreSQL testing ecosystem, and the choice depends on your project's
specific requirements and architectural preferences.
