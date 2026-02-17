export const EXAMPLE_DC = `variable "region" {
  type    = string
  default = "us-east-1"
}

environment {
  database {
    when = element(split(":", node.inputs.type), 0) == "postgres"

    module "postgres" {
      plugin = "native"
      build  = "./modules/docker-postgres"
      inputs = {
        name = "\${environment.name}-\${node.component}-\${node.name}"
      }
    }

    outputs = {
      host     = module.postgres.host
      port     = module.postgres.port
      url      = module.postgres.url
      database = module.postgres.database
      username = module.postgres.username
      password = module.postgres.password
    }
  }

  database {
    when = element(split(":", node.inputs.type), 0) == "redis"

    module "redis" {
      plugin = "native"
      build  = "./modules/docker-redis"
      inputs = {
        name = "\${environment.name}-\${node.component}-\${node.name}"
      }
    }

    outputs = {
      host = module.redis.host
      port = module.redis.port
      url  = module.redis.url
    }
  }

  database {
    error = "Unsupported database type: \${node.inputs.type}. Use postgres or redis."
  }

  deployment {
    module "deployment" {
      plugin = "native"
      build  = "./modules/docker-deployment"
      inputs = {
        name  = "\${environment.name}-\${node.component}-\${node.name}"
        image = node.inputs.image
      }
    }

    outputs = {
      id = module.deployment.id
    }
  }

  function {
    module "function" {
      plugin = "native"
      build  = "./modules/process-function"
      inputs = {
        name = "\${environment.name}-\${node.component}-\${node.name}"
      }
    }

    outputs = {
      id       = module.function.id
      endpoint = module.function.endpoint
    }
  }

  service {
    module "service" {
      plugin = "native"
      build  = "./modules/docker-service"
      inputs = {
        name = "\${environment.name}-\${node.component}-\${node.name}"
      }
    }

    outputs = {
      host = module.service.host
      port = module.service.port
      url  = module.service.url
    }
  }

  route {
    module "route" {
      plugin = "native"
      build  = "./modules/local-route"
      inputs = {
        subdomain = node.inputs.subdomain
      }
    }

    outputs = {
      url  = module.route.url
      host = module.route.host
      port = module.route.port
    }
  }

  bucket {
    module "bucket" {
      plugin = "native"
      build  = "./modules/minio-bucket"
      inputs = {
        name = "\${environment.name}-\${node.component}-\${node.name}"
      }
    }

    outputs = {
      endpoint       = module.bucket.endpoint
      bucket         = module.bucket.bucket
      accessKeyId    = module.bucket.access_key_id
      secretAccessKey = module.bucket.secret_access_key
    }
  }
}`;

export const EXAMPLE_COMP = `databases:
  main:
    type: postgres:16
  cache:
    type: redis:7

builds:
  api:
    context: ./api

deployments:
  api:
    image: \${{ builds.api.image }}
    environment:
      DATABASE_URL: \${{ databases.main.url }}
      REDIS_URL: \${{ databases.cache.url }}

services:
  api:
    deployment: api
    port: 8080

routes:
  main:
    type: http
    service: api`;

export const EXAMPLE_OP_ENV = `components:
  my-app:
    image: ghcr.io/myorg/my-app:latest`;

export const EXAMPLE_OP_ENV_COMP = `databases:
  main:
    type: postgres:16

buckets:
  uploads:
    archiveStorage: true

deployments:
  api:
    command: ["npm", "start"]
    environment:
      DATABASE_URL: \${{ databases.main.url }}
      S3_ENDPOINT: \${{ buckets.uploads.endpoint }}

services:
  api:
    deployment: api
    port: 3000

routes:
  main:
    type: http
    service: api`;

export const OperatorPlayground = () => {
  const [dcHCL, setDcHCL] = React.useState(EXAMPLE_DC);
  const [mode, setMode] = React.useState("component");
  const [yamlInput, setYamlInput] = React.useState(EXAMPLE_COMP);
  const [envYaml, setEnvYaml] = React.useState(EXAMPLE_OP_ENV);
  const [compYamls, setCompYamls] = React.useState({
    "my-app": EXAMPLE_OP_ENV_COMP,
  });
  const [variables, setVariables] = React.useState([{ key: "region", value: "us-east-1" }]);
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

      const varsObj = {};
      variables.forEach((v) => {
        if (v.key) varsObj[v.key] = v.value;
      });

      const yaml = mode === "component" ? yamlInput : envYaml;
      const compMap = mode === "environment" ? JSON.stringify(compYamls) : "";
      const varsJSON = JSON.stringify(varsObj);

      const raw = window.cldctlParseInfrastructure(dcHCL, mode, yaml, compMap, varsJSON);
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

  const addVariable = () => {
    setVariables([...variables, { key: "", value: "" }]);
  };

  const updateVariable = (index, field, val) => {
    const copy = [...variables];
    copy[index] = { ...copy[index], [field]: val };
    setVariables(copy);
  };

  const removeVariable = (index) => {
    setVariables(variables.filter((_, i) => i !== index));
  };

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

  const inputStyle = {
    width: "100%",
    padding: "0.5rem 0.75rem",
    fontSize: "0.875rem",
    border: "1px solid #e2e8f0",
    borderRadius: "6px",
    outline: "none",
    fontFamily: "inherit",
  };

  const textareaStyle = {
    width: "100%",
    padding: "0.75rem",
    fontSize: "0.8125rem",
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    border: "1px solid #e2e8f0",
    borderRadius: "6px",
    resize: "vertical",
    lineHeight: 1.5,
    outline: "none",
    background: "#f8fafc",
  };

  const labelStyle = {
    display: "block",
    fontSize: "0.8125rem",
    fontWeight: 600,
    marginBottom: "0.375rem",
    color: "#334155",
  };

  return (
    <div style={{ fontFamily: "var(--font-sans, system-ui, sans-serif)" }}>
      {/* Two-panel layout */}
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "1fr 1fr",
          gap: "1.5rem",
          marginBottom: "1.5rem",
        }}
      >
        {/* Left Panel - Datacenter */}
        <div>
          <div
            style={{
              fontSize: "0.9375rem",
              fontWeight: 700,
              marginBottom: "0.75rem",
              color: "#1e293b",
            }}
          >
            Datacenter Template
          </div>

          <div style={{ marginBottom: "0.75rem" }}>
            <label style={labelStyle}>Datacenter HCL</label>
            <textarea
              value={dcHCL}
              onChange={(e) => setDcHCL(e.target.value)}
              spellCheck={false}
              style={{ ...textareaStyle, minHeight: "400px" }}
            />
          </div>

          {/* Variables */}
          <div style={{ marginBottom: "0.75rem" }}>
            <div
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                marginBottom: "0.5rem",
              }}
            >
              <label style={{ ...labelStyle, marginBottom: 0 }}>Variables</label>
              <button
                onClick={addVariable}
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
                + Add
              </button>
            </div>
            {variables.map((v, i) => (
              <div
                key={i}
                style={{
                  display: "flex",
                  gap: "0.5rem",
                  marginBottom: "0.375rem",
                  alignItems: "center",
                }}
              >
                <input
                  type="text"
                  placeholder="key"
                  value={v.key}
                  onChange={(e) => updateVariable(i, "key", e.target.value)}
                  style={{ ...inputStyle, maxWidth: "40%" }}
                />
                <input
                  type="text"
                  placeholder="value"
                  value={v.value}
                  onChange={(e) => updateVariable(i, "value", e.target.value)}
                  style={inputStyle}
                />
                <button
                  onClick={() => removeVariable(i)}
                  style={{
                    padding: "0.375rem 0.5rem",
                    fontSize: "0.75rem",
                    cursor: "pointer",
                    border: "1px solid #fca5a5",
                    borderRadius: "4px",
                    background: "#fef2f2",
                    color: "#dc2626",
                    whiteSpace: "nowrap",
                  }}
                >
                  x
                </button>
              </div>
            ))}
          </div>
        </div>

        {/* Right Panel - Component/Environment */}
        <div>
          <div
            style={{
              fontSize: "0.9375rem",
              fontWeight: 700,
              marginBottom: "0.75rem",
              color: "#1e293b",
            }}
          >
            Application Config
          </div>

          {/* Mode Toggle */}
          <div
            style={{
              display: "flex",
              gap: "0",
              marginBottom: "0.75rem",
              borderRadius: "8px",
              overflow: "hidden",
              border: "1px solid #e2e8f0",
              width: "fit-content",
            }}
          >
            <button
              onClick={() => setMode("component")}
              style={{
                padding: "0.375rem 1rem",
                fontSize: "0.8125rem",
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
                padding: "0.375rem 1rem",
                fontSize: "0.8125rem",
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

          {mode === "component" && (
            <div style={{ marginBottom: "0.75rem" }}>
              <label style={labelStyle}>Component YAML</label>
              <textarea
                value={yamlInput}
                onChange={(e) => setYamlInput(e.target.value)}
                spellCheck={false}
                style={{ ...textareaStyle, minHeight: "360px" }}
              />
            </div>
          )}

          {mode === "environment" && (
            <div>
              <div style={{ marginBottom: "0.75rem" }}>
                <label style={labelStyle}>Environment YAML</label>
                <textarea
                  value={envYaml}
                  onChange={(e) => setEnvYaml(e.target.value)}
                  spellCheck={false}
                  style={{ ...textareaStyle, minHeight: "120px" }}
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
                  <label style={{ ...labelStyle, marginBottom: 0 }}>
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
                    + Add
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
                          fontFamily:
                            "ui-monospace, SFMono-Regular, Menlo, monospace",
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
                        ...textareaStyle,
                        minHeight: "180px",
                        border: "none",
                        borderRadius: 0,
                      }}
                    />
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>

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
        {loading ? "Loading WASM..." : "Visualize Infrastructure"}
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
              <strong style={{ color: "#334155" }}>{result.nodes}</strong> resources
            </span>
            <span>
              <strong style={{ color: "#334155" }}>{result.modules}</strong> modules
            </span>
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

          {/* Mapping Table */}
          {result.mappings && result.mappings.length > 0 && (
            <div style={{ marginBottom: "1rem" }}>
              <div
                style={{
                  fontSize: "0.875rem",
                  fontWeight: 600,
                  marginBottom: "0.5rem",
                  color: "#334155",
                }}
              >
                Hook Mappings
              </div>
              <div
                style={{
                  border: "1px solid #e2e8f0",
                  borderRadius: "6px",
                  overflow: "hidden",
                }}
              >
                <table
                  style={{
                    width: "100%",
                    borderCollapse: "collapse",
                    fontSize: "0.8125rem",
                  }}
                >
                  <thead>
                    <tr style={{ background: "#f1f5f9" }}>
                      <th
                        style={{
                          padding: "0.5rem 0.75rem",
                          textAlign: "left",
                          fontWeight: 600,
                          borderBottom: "1px solid #e2e8f0",
                        }}
                      >
                        Resource
                      </th>
                      <th
                        style={{
                          padding: "0.5rem 0.75rem",
                          textAlign: "left",
                          fontWeight: 600,
                          borderBottom: "1px solid #e2e8f0",
                        }}
                      >
                        Type
                      </th>
                      <th
                        style={{
                          padding: "0.5rem 0.75rem",
                          textAlign: "left",
                          fontWeight: 600,
                          borderBottom: "1px solid #e2e8f0",
                        }}
                      >
                        Modules / Error
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {result.mappings.map((m, i) => (
                      <tr
                        key={i}
                        style={{
                          background: m.isError ? "#fef2f2" : i % 2 === 0 ? "#fff" : "#f8fafc",
                        }}
                      >
                        <td
                          style={{
                            padding: "0.5rem 0.75rem",
                            borderBottom: "1px solid #e2e8f0",
                            fontFamily:
                              "ui-monospace, SFMono-Regular, Menlo, monospace",
                          }}
                        >
                          {m.nodeId}
                        </td>
                        <td
                          style={{
                            padding: "0.5rem 0.75rem",
                            borderBottom: "1px solid #e2e8f0",
                          }}
                        >
                          {m.nodeType}
                        </td>
                        <td
                          style={{
                            padding: "0.5rem 0.75rem",
                            borderBottom: "1px solid #e2e8f0",
                            color: m.isError ? "#dc2626" : "inherit",
                          }}
                        >
                          {m.isError
                            ? m.error
                            : m.modules
                            ? m.modules.join(", ")
                            : "-"}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

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
