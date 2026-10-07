// Copy buttons for the snippets the panel hands the operator.
//
// The page works without this file: every block stays selectable text. The
// script only removes the tedium of selecting a long credential by hand, which
// is where people lose or truncate it.
(function () {
  "use strict";

  function label(button, text, restoreAfter) {
    button.textContent = text;
    if (restoreAfter) {
      window.setTimeout(function () {
        button.textContent = "Copiar";
        button.classList.remove("copy--done", "copy--failed");
      }, 2000);
    }
  }

  function copy(text, button) {
    if (!navigator.clipboard) {
      button.classList.add("copy--failed");
      label(button, "Selecione e copie", true);
      return;
    }
    navigator.clipboard.writeText(text).then(
      function () {
        button.classList.add("copy--done");
        label(button, "Copiado", true);
      },
      function () {
        button.classList.add("copy--failed");
        label(button, "Falhou", true);
      }
    );
  }

  // While the checklist waits for a client to connect, ask the server whether
  // it has. The server already knows: any authenticated MCP request stamps the
  // key it was made with. Polling only runs while that step is open, and stops
  // as soon as it closes, so an idle page costs nothing.
  var waiting = document.querySelector("[data-progress][data-connected='false']");
  if (waiting) {
    var attempts = 0;
    var timer = window.setInterval(function () {
      attempts += 1;
      // Give up after roughly ten minutes: by then the page is stale anyway and
      // a reload is the honest way back.
      if (attempts > 150) {
        window.clearInterval(timer);
        return;
      }
      fetch("/api/progresso", { headers: { Accept: "application/json" } })
        .then(function (response) {
          return response.ok ? response.json() : null;
        })
        .then(function (state) {
          if (state && state.client_connected) {
            window.clearInterval(timer);
            window.location.reload();
          }
        })
        .catch(function () {
          /* A failed poll is not worth reporting: the next one may succeed. */
        });
    }, 4000);
  }

  // A tool's steps end by themselves: the page asks whether the key it just
  // created has been used, and comes back as the "connected" page once it has.
  var waitKey = document.querySelector("[data-wait-key]");
  if (waitKey) {
    var keyID = waitKey.getAttribute("data-wait-key");
    var tool = waitKey.getAttribute("data-wait-tool");
    var keyPolls = 0;
    var keyWatch = window.setInterval(function () {
      if (++keyPolls > 150) {
        window.clearInterval(keyWatch);
        return;
      }
      fetch("/api/progresso?id=" + encodeURIComponent(keyID), { headers: { Accept: "application/json" }, cache: "no-store" })
        .then(function (response) {
          return response.ok ? response.json() : null;
        })
        .then(function (state) {
          if (state && state.used) {
            window.clearInterval(keyWatch);
            window.location.replace("/conectar/" + encodeURIComponent(tool) + "?conectado=" + encodeURIComponent(keyID));
          }
        })
        .catch(function () {
          /* A failed poll is not worth reporting: the next one may succeed. */
        });
    }, 4000);
  }

  // The installation wizard waits on the one thing no automation can do: the
  // operator clicking a link in their own inbox. Asking the server which step
  // is open turns that wait into the page moving on by itself, instead of a
  // reload nobody knows to perform. The pairing step reloads on its own, so
  // only the licence step carries the marker.
  var wizard = document.querySelector("[data-onboarding]");
  if (wizard) {
    var openStep = wizard.getAttribute("data-onboarding");
    var polls = 0;
    var watch = window.setInterval(function () {
      polls += 1;
      if (polls > 150) {
        window.clearInterval(watch);
        return;
      }
      fetch("/api/instalacao", { headers: { Accept: "application/json" } })
        .then(function (response) {
          return response.ok ? response.json() : null;
        })
        .then(function (state) {
          if (state && String(state.step) !== openStep) {
            window.clearInterval(watch);
            window.location.reload();
          }
        })
        .catch(function () {
          /* A failed poll is not worth reporting: the next one may succeed. */
        });
    }, 4000);
  }

  // Some of these forms wait on WhatsApp: creating an instance only answers
  // once Evolution has the session up, which is several seconds of a page that
  // looks idle. People read that as a dead click and press the button again,
  // which creates a second instance. Marking the form busy says the click
  // landed. The page still works without this file: the button is a plain
  // submit and the browser's own progress bar is the fallback.
  document.querySelectorAll("form[data-busy]").forEach(function (form) {
    form.addEventListener("submit", function (event) {
      if (form.dataset.state === "busy") {
        event.preventDefault();
        return;
      }
      form.dataset.state = "busy";
      form.setAttribute("aria-busy", "true");
      var note = form.querySelector("[data-busy-note]");
      if (note) {
        note.hidden = false;
      }
      var button = form.querySelector("button[type=submit]") || form.querySelector("button");
      // Disabling happens after this handler returns: a control disabled
      // during the submit event can be left out of the request body, and the
      // fields are what the server is being asked about.
      window.setTimeout(function () {
        form.querySelectorAll("a.btn").forEach(function (link) {
          link.setAttribute("aria-disabled", "true");
          link.tabIndex = -1;
        });
        if (!button) {
          return;
        }
        button.disabled = true;
        button.textContent = form.getAttribute("data-busy") || "Aguarde";
        var spinner = document.createElement("span");
        spinner.className = "spinner";
        spinner.setAttribute("aria-hidden", "true");
        button.insertBefore(spinner, button.firstChild);
      }, 0);
    });
  });

  document.querySelectorAll("[data-copy]").forEach(function (block) {
    var source = block.querySelector("code") || block;
    var button = document.createElement("button");
    button.type = "button";
    button.className = "copy";
    button.textContent = "Copiar";
    button.addEventListener("click", function () {
      copy(source.textContent, button);
    });

    // The button belongs in the snippet's own header when there is one, so it
    // never sits over the code. Snippets without a header get it overlaid in
    // the corner instead, which is why that case is marked for the stylesheet.
    var snippet = block.closest(".snippet");
    var head = snippet && snippet.querySelector(".snippet__head");
    if (head) {
      head.appendChild(button);
      return;
    }
    if (snippet) {
      snippet.classList.add("snippet--loose");
      snippet.insertBefore(button, block);
      return;
    }
    block.parentNode.insertBefore(button, block);
  });
})();

// The update page keeps asking how the update is going. The gateway restarts
// in the middle of it, so failed requests are expected and simply retried, and
// the new version is recognised by the version the server reports — the old
// session cookie no longer opens anything once the process is new.
(function () {
  var card = document.querySelector("[data-update-status]");
  if (!card) return;
  var target = card.getAttribute("data-target");
  if (!target) return;
  var state = card.querySelector("[data-update-state]");
  var message = card.querySelector("[data-update-message]");
  var log = card.querySelector("[data-update-log]");
  var running = card.querySelector("[data-update-running]");
  var labels = { queued: "Na fila", running: "Em andamento", succeeded: "Concluída", failed: "Falhou" };
  var tones = { queued: "warn", running: "warn", succeeded: "ok", failed: "off" };
  var done = false;

  function show(status) {
    if (state && status.state) {
      state.textContent = labels[status.state] || status.state;
      state.className = "pill pill--" + (tones[status.state] || "warn");
    }
    if (message && (status.message || status.phase)) message.textContent = status.message || status.phase;
    if (log && status.log && status.log.length) {
      log.hidden = false;
      log.textContent = "";
      status.log.forEach(function (line) {
        var item = document.createElement("li");
        item.textContent = line;
        log.appendChild(item);
      });
      log.scrollTop = log.scrollHeight;
    }
  }

  function finish(text, tone) {
    done = true;
    if (state) {
      state.textContent = tone === "ok" ? labels.succeeded : labels.failed;
      state.className = "pill pill--" + tone;
    }
    if (message) message.textContent = text;
  }

  function poll() {
    if (done) return;
    fetch("/api/atualizacao", { cache: "no-store", credentials: "same-origin" })
      .then(function (response) { return response.json(); })
      .then(function (body) {
        if (running && body.version) running.textContent = body.version;
        if (body.version === target) {
          finish("A versão " + target + " está rodando." + (body.signed_in ? "" : " Entre de novo para continuar."), "ok");
          if (!body.signed_in) setTimeout(function () { window.location.href = "/login"; }, 2500);
          return;
        }
        if (body.status) {
          show(body.status);
          if (body.status.state === "failed") { done = true; return; }
        }
      })
      .catch(function () {
        if (message) message.textContent = "O painel está reiniciando com a nova versão…";
      })
      .then(function () { if (!done) setTimeout(poll, 3000); });
  }
  setTimeout(poll, 2000);
})();
