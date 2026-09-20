// The show/hide control on password fields.
//
// Added from here rather than written into the templates, so a field that is
// still a plain password input when this file does not load keeps working
// exactly as before — the control is an enhancement, never the thing standing
// between someone and their own panel. Every password input on the page gets
// one, including any added later.
(function () {
  "use strict";

  // Two paths rather than an icon font or an SVG sprite: one eye, one eye with
  // a stroke through it. Both inherit currentColor.
  var SHOWN =
    '<svg viewBox="0 0 20 20" width="18" height="18" aria-hidden="true" focusable="false">' +
    '<path fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" ' +
    'd="M1.8 10S4.9 4.6 10 4.6 18.2 10 18.2 10 15.1 15.4 10 15.4 1.8 10 1.8 10Z"/>' +
    '<circle cx="10" cy="10" r="2.6" fill="none" stroke="currentColor" stroke-width="1.6"/></svg>';
  var HIDDEN =
    '<svg viewBox="0 0 20 20" width="18" height="18" aria-hidden="true" focusable="false">' +
    '<path fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" ' +
    'd="M1.8 10S4.9 4.6 10 4.6 18.2 10 18.2 10 15.1 15.4 10 15.4 1.8 10 1.8 10Z"/>' +
    '<circle cx="10" cy="10" r="2.6" fill="none" stroke="currentColor" stroke-width="1.6"/>' +
    '<path stroke="currentColor" stroke-width="1.6" stroke-linecap="round" d="M3.5 16.5 16.5 3.5"/></svg>';

  document.querySelectorAll('input[type=password]').forEach(function (field) {
    var wrap = document.createElement("div");
    wrap.className = "reveal";
    field.parentNode.insertBefore(wrap, field);
    wrap.appendChild(field);

    var button = document.createElement("button");
    // Not a submit button: a click here must never post the form, and Enter
    // while it has focus must not either.
    button.type = "button";
    button.className = "reveal__toggle";
    button.innerHTML = HIDDEN;
    button.setAttribute("aria-label", "Mostrar a senha");
    button.setAttribute("aria-pressed", "false");
    button.title = "Mostrar a senha";

    button.addEventListener("click", function () {
      var showing = field.type === "text";
      field.type = showing ? "password" : "text";
      button.innerHTML = showing ? HIDDEN : SHOWN;
      var label = showing ? "Mostrar a senha" : "Ocultar a senha";
      button.setAttribute("aria-label", label);
      button.setAttribute("aria-pressed", showing ? "false" : "true");
      button.title = label;
      // The caret goes back where the person was typing, which is what makes
      // this usable mid-password rather than only before or after.
      var at = field.value.length;
      field.focus();
      try {
        field.setSelectionRange(at, at);
      } catch (error) {
        // A browser that refuses setSelectionRange on this input type is not a
        // reason to leave the field unfocused.
      }
    });

    wrap.appendChild(button);
  });
})();
