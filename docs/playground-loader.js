// Lazy WASM loader for the cldctl playground.
// Included globally by Mintlify, but only loads the WASM binary
// when a playground page explicitly calls window._cldctlInit().
(function () {
  var loading = null;

  window._cldctlInit = function () {
    if (loading) return loading;

    loading = new Promise(function (resolve, reject) {
      // Load wasm_exec.js (Go's WASM support runtime)
      var script = document.createElement("script");
      script.src = "/assets/wasm_exec.js";
      script.onload = function () {
        // Instantiate and run the Go WASM module
        var go = new Go();
        WebAssembly.instantiateStreaming(
          fetch("/assets/playground.wasm"),
          go.importObject
        )
          .then(function (result) {
            // Set up a promise that resolves when Go signals readiness
            var readyPromise = new Promise(function (readyResolve) {
              window._cldctlReady = readyResolve;
            });
            go.run(result.instance);
            return readyPromise;
          })
          .then(resolve)
          .catch(reject);
      };
      script.onerror = function () {
        reject(new Error("Failed to load wasm_exec.js"));
      };
      document.head.appendChild(script);
    });

    return loading;
  };
})();
