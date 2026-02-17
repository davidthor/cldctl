export const EXAMPLE_COMPONENT = `databases:
  main:
    type: postgres:16

builds:
  api:
    context: ./api

deployments:
  api:
    image: \${{ builds.api.image }}
    environment:
      DATABASE_URL: \${{ databases.main.url }}

services:
  api:
    deployment: api
    port: 8080

routes:
  main:
    type: http
    service: api`;

export const EXAMPLE_ENV = `components:
  my-app:
    image: ghcr.io/myorg/my-app:latest
  auth:
    image: ghcr.io/myorg/auth:latest`;

export const EXAMPLE_ENV_COMP_MYAPP = `databases:
  main:
    type: postgres:16

deployments:
  api:
    command: ["npm", "run", "dev"]
    environment:
      DATABASE_URL: \${{ databases.main.url }}

services:
  api:
    deployment: api
    port: 3000

routes:
  main:
    type: http
    service: api`;

export const EXAMPLE_ENV_COMP_AUTH = `databases:
  users:
    type: postgres:16

deployments:
  api:
    command: ["npm", "start"]
    environment:
      DATABASE_URL: \${{ databases.users.url }}

services:
  api:
    deployment: api
    port: 4000`;

export const DeveloperPlayground = () => {
  const [mode, setMode] = React.useState("component");
  const [yamlInput, setYamlInput] = React.useState(EXAMPLE_COMPONENT);
  const [compName, setCompName] = React.useState("my-app");
  const [envYaml, setEnvYaml] = React.useState(EXAMPLE_ENV);
  const [compYamls, setCompYamls] = React.useState({
    "my-app": EXAMPLE_ENV_COMP_MYAPP,
    auth: EXAMPLE_ENV_COMP_AUTH,
  });
  const [result, setResult] = React.useState(null);
  const [loading, setLoading] = React.useState(false);
  const [wasmError, setWasmError] = React.useState(null);
  const mermaidRef = React.useRef(null);

  const handleVisualize = async () => {
    setLoading(true);
    setResult(null);
    setWasmError(null);

    try {
      if (!window._cldctlInit) {
        setWasmError("Playground module not available. Please try refreshing the page.");
        setLoading(false);
        return;
      }

      await window._cldctlInit();

      let raw;
      if (mode === "component") {
        raw = window.cldctlParseComponent(yamlInput, compName);
      } else {
        const compMap = JSON.stringify(compYamls);
        raw = window.cldctlParseEnvironment(envYaml, compMap);
      }

      const parsed = JSON.parse(raw);
      setResult(parsed);
    } catch (err) {
      setWasmError(err.message || String(err));
    } finally {
      setLoading(false);
    }
  };

  // Render mermaid diagram when result changes
  React.useEffect(() => {
    if (!result || !result.mermaid || !mermaidRef.current) return;

    const renderDiagram = async () => {
      try {
        if (!window.mermaid) {
          const script = document.createElement("script");
          script.src = "https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js";
          document.head.appendChild(script);
          await new Promise((res) => (script.onload = res));
          window.mermaid.initialize({ startOnLoad: false, theme: "default" });
        }

        mermaidRef.current.innerHTML = "";
        const id = "mermaid-" + Date.now();
        const { svg } = await window.mermaid.render(id, result.mermaid);
        mermaidRef.current.innerHTML = svg;
      } catch (e) {
        if (mermaidRef.current) {
          mermaidRef.current.innerHTML =
            '<pre style="color:#ef4444;white-space:pre-wrap">Mermaid render error: ' +
            (e.message || e) +
            "</pre>";
        }
      }
    };

    renderDiagram();
  }, [result]);

  const addCompYaml = (name) => {
    if (name && !compYamls[name]) {
      setCompYamls({ ...compYamls, [name]: "" });
    }
  };

  const updateCompYaml = (name, value) => {
    setCompYamls({ ...compYamls, [name]: value });
  };

  const removeCompYaml = (name) => {
    const copy = { ...compYamls };
    delete copy[name];
    setCompYamls(copy);
  };

  return (
    <div style={{ fontFamily: "var(--font-sans, system-ui, sans-serif)" }}>
      {/* Mode Toggle */}
      <div
        style={{
          display: "flex",
          gap: "0",
          marginBottom: "1.5rem",
          borderRadius: "8px",
          overflow: "hidden",
          border: "1px solid #e2e8f0",
          width: "fit-content",
        }}
      >
        <button
          onClick={() => setMode("component")}
          style={{
            padding: "0.5rem 1.25rem",
            fontSize: "0.875rem",
            fontWeight: 500,
            cursor: "pointer",
            border: "none",
            background: mode === "component" ? "#6366f1" : "#f8fafc",
            color: mode === "component" ? "#fff" : "#64748b",
            transition: "all 0.15s",
          }}
        >
          Component
        </button>
        <button
          onClick={() => setMode("environment")}
          style={{
            padding: "0.5rem 1.25rem",
            fontSize: "0.875rem",
            fontWeight: 500,
            cursor: "pointer",
            border: "none",
            borderLeft: "1px solid #e2e8f0",
            background: mode === "environment" ? "#6366f1" : "#f8fafc",
            color: mode === "environment" ? "#fff" : "#64748b",
            transition: "all 0.15s",
          }}
        >
          Environment
        </button>
      </div>

      {/* Component Mode */}
      {mode === "component" && (
        <div>
          <div style={{ marginBottom: "0.75rem" }}>
            <label
              style={{
                display: "block",
                fontSize: "0.8125rem",
                fontWeight: 600,
                marginBottom: "0.375rem",
                color: "#334155",
              }}
            >
              Component Name
            </label>
            <input
              type="text"
              value={compName}
              onChange={(e) => setCompName(e.target.value)}
              placeholder="my-app"
              style={{
                width: "100%",
                maxWidth: "300px",
                padding: "0.5rem 0.75rem",
                fontSize: "0.875rem",
                border: "1px solid #e2e8f0",
                borderRadius: "6px",
                outline: "none",
                fontFamily: "inherit",
              }}
            />
          </div>
          <div style={{ marginBottom: "0.75rem" }}>
            <label
              style={{
                display: "block",
                fontSize: "0.8125rem",
                fontWeight: 600,
                marginBottom: "0.375rem",
                color: "#334155",
              }}
            >
              Component YAML
            </label>
            <textarea
              value={yamlInput}
              onChange={(e) => setYamlInput(e.target.value)}
              spellCheck={false}
              style={{
                width: "100%",
                minHeight: "280px",
                padding: "0.75rem",
                fontSize: "0.8125rem",
                fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
                border: "1px solid #e2e8f0",
                borderRadius: "6px",
                resize: "vertical",
                lineHeight: 1.5,
                outline: "none",
                background: "#f8fafc",
              }}
            />
          </div>
        </div>
      )}

      {/* Environment Mode */}
      {mode === "environment" && (
        <div>
          <div style={{ marginBottom: "0.75rem" }}>
            <label
              style={{
                display: "block",
                fontSize: "0.8125rem",
                fontWeight: 600,
                marginBottom: "0.375rem",
                color: "#334155",
              }}
            >
              Environment YAML
            </label>
            <textarea
              value={envYaml}
              onChange={(e) => setEnvYaml(e.target.value)}
              spellCheck={false}
              style={{
                width: "100%",
                minHeight: "160px",
                padding: "0.75rem",
                fontSize: "0.8125rem",
                fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
                border: "1px solid #e2e8f0",
                borderRadius: "6px",
                resize: "vertical",
                lineHeight: 1.5,
                outline: "none",
                background: "#f8fafc",
              }}
            />
          </div>

          <div style={{ marginBottom: "0.75rem" }}>
            <div
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                marginBottom: "0.5rem",
              }}
            >
              <label
                style={{ fontSize: "0.8125rem", fontWeight: 600, color: "#334155" }}
              >
                Component YAMLs
              </label>
              <button
                onClick={() => {
                  const name = prompt("Component name:");
                  if (name) addCompYaml(name);
                }}
                style={{
                  padding: "0.25rem 0.75rem",
                  fontSize: "0.75rem",
                  fontWeight: 500,
                  cursor: "pointer",
                  border: "1px solid #e2e8f0",
                  borderRadius: "6px",
                  background: "#f8fafc",
                  color: "#6366f1",
                }}
              >
                + Add Component
              </button>
            </div>

            {Object.entries(compYamls).map(([name, yaml]) => (
              <div
                key={name}
                style={{
                  marginBottom: "0.75rem",
                  border: "1px solid #e2e8f0",
                  borderRadius: "6px",
                  overflow: "hidden",
                }}
              >
                <div
                  style={{
                    display: "flex",
                    justifyContent: "space-between",
                    alignItems: "center",
                    padding: "0.5rem 0.75rem",
                    background: "#f1f5f9",
                    borderBottom: "1px solid #e2e8f0",
                  }}
                >
                  <span
                    style={{
                      fontSize: "0.8125rem",
                      fontWeight: 600,
                      fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
                      color: "#334155",
                    }}
                  >
                    {name}
                  </span>
                  <button
                    onClick={() => removeCompYaml(name)}
                    style={{
                      padding: "0.125rem 0.5rem",
                      fontSize: "0.75rem",
                      cursor: "pointer",
                      border: "1px solid #fca5a5",
                      borderRadius: "4px",
                      background: "#fef2f2",
                      color: "#dc2626",
                    }}
                  >
                    Remove
                  </button>
                </div>
                <textarea
                  value={yaml}
                  onChange={(e) => updateCompYaml(name, e.target.value)}
                  spellCheck={false}
                  style={{
                    width: "100%",
                    minHeight: "200px",
                    padding: "0.75rem",
                    fontSize: "0.8125rem",
                    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
                    border: "none",
                    resize: "vertical",
                    lineHeight: 1.5,
                    outline: "none",
                    background: "#f8fafc",
                  }}
                />
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Visualize Button */}
      <button
        onClick={handleVisualize}
        disabled={loading}
        style={{
          padding: "0.625rem 1.5rem",
          fontSize: "0.875rem",
          fontWeight: 600,
          cursor: loading ? "wait" : "pointer",
          border: "none",
          borderRadius: "6px",
          background: loading ? "#94a3b8" : "#6366f1",
          color: "#fff",
          marginBottom: "1.5rem",
          transition: "background 0.15s",
        }}
      >
        {loading ? "Loading WASM..." : "Visualize"}
      </button>

      {/* Error Display */}
      {wasmError && (
        <div
          style={{
            padding: "0.75rem 1rem",
            marginBottom: "1rem",
            borderRadius: "6px",
            border: "1px solid #fca5a5",
            background: "#fef2f2",
            color: "#dc2626",
            fontSize: "0.8125rem",
          }}
        >
          {wasmError}
        </div>
      )}

      {result && result.errors && result.errors.length > 0 && (
        <div
          style={{
            padding: "0.75rem 1rem",
            marginBottom: "1rem",
            borderRadius: "6px",
            border: "1px solid #fde68a",
            background: "#fffbeb",
            color: "#92400e",
            fontSize: "0.8125rem",
          }}
        >
          <strong>Warnings:</strong>
          <ul style={{ margin: "0.25rem 0 0 1.25rem", padding: 0 }}>
            {result.errors.map((e, i) => (
              <li key={i}>{e}</li>
            ))}
          </ul>
        </div>
      )}

      {/* Results */}
      {result && result.mermaid && (
        <div>
          {/* Stats Bar */}
          <div
            style={{
              display: "flex",
              gap: "1.5rem",
              marginBottom: "1rem",
              fontSize: "0.8125rem",
              color: "#64748b",
            }}
          >
            <span>
              <strong style={{ color: "#334155" }}>{result.nodes}</strong> nodes
            </span>
            <span>
              <strong style={{ color: "#334155" }}>{result.edges}</strong> edges
            </span>
            {result.components && (
              <span>
                <strong style={{ color: "#334155" }}>{result.components.length}</strong>{" "}
                components
              </span>
            )}
          </div>

          {/* Mermaid Diagram */}
          <div
            style={{
              border: "1px solid #e2e8f0",
              borderRadius: "8px",
              padding: "1.5rem",
              background: "#fff",
              overflow: "auto",
              marginBottom: "1rem",
            }}
          >
            <div ref={mermaidRef} />
          </div>

          {/* Raw Mermaid Source */}
          <details style={{ marginBottom: "1rem" }}>
            <summary
              style={{
                cursor: "pointer",
                fontSize: "0.8125rem",
                color: "#64748b",
                marginBottom: "0.5rem",
              }}
            >
              View Mermaid source
            </summary>
            <pre
              style={{
                padding: "0.75rem",
                background: "#f8fafc",
                border: "1px solid #e2e8f0",
                borderRadius: "6px",
                fontSize: "0.75rem",
                overflow: "auto",
                lineHeight: 1.5,
              }}
            >
              {result.mermaid}
            </pre>
          </details>
        </div>
      )}
    </div>
  );
};
