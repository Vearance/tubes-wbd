// Script for login page 
  
(() => {
  "use strict";

  const form = document.querySelector('form[action="/login"]');
  if (!(form instanceof HTMLFormElement)) {
    return;
  }

  const submitButton = form.querySelector("#login-submit");
  const submitLabel = form.querySelector("[data-submit-label]");
  const submitSpinner = form.querySelector("[data-submit-spinner]");

  form.addEventListener("submit", (event) => {
    if (!form.checkValidity()) {
      event.preventDefault();
      form.classList.add("was-validated");
      form.querySelector(":invalid")?.focus();
      return;
    }

    if (submitButton instanceof HTMLButtonElement) {
      submitButton.disabled = true;
      submitButton.setAttribute("aria-busy", "true");
    }

    if (submitLabel instanceof HTMLElement) {
      submitLabel.textContent = "Signing in…";
    }

    submitSpinner?.classList.remove("d-none");
  });
})();
