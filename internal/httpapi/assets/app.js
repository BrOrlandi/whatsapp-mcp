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
