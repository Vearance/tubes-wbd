(() => {
  const groupForm = document.getElementById('group-form');
  if (groupForm) {
    const memberRows = document.getElementById('member-rows');
    const accessRows = document.getElementById('access-rows');
    const memberChoice = document.getElementById('member-choice');
    const databaseChoice = document.getElementById('database-choice');
    const updateEmpty = () => {
      document.getElementById('members-empty').hidden = memberRows.children.length > 0;
      document.getElementById('access-empty').hidden = accessRows.children.length > 0;
    };
    const cell = (row, text) => { const td = row.insertCell(); td.textContent = text; return td; };
    const removeButton = (row, kind) => {
      const button = document.createElement('button');
      button.type = 'button'; button.className = 'btn btn-outline-secondary btn-sm btn-with-icon';
      button.dataset.remove = kind;
      button.innerHTML = '<svg class="button-icon" aria-hidden="true"><use href="/static/assets/bootstrap-icons.svg#trash"></use></svg><span>Remove</span>';
      row.insertCell().append(button);
    };
    document.getElementById('add-member').addEventListener('click', () => {
      const username = memberChoice.value;
      if (!username || [...memberRows.children].some(row => row.dataset.member === username)) return;
      const row = memberRows.insertRow(); row.dataset.member = username;
      const nameCell = cell(row, username);
      const hidden = document.createElement('input'); hidden.type = 'hidden'; hidden.name = 'member'; hidden.value = username; nameCell.append(hidden);
      cell(row, memberChoice.selectedOptions[0].dataset.role || 'User'); removeButton(row, 'member');
      memberChoice.value = ''; updateEmpty();
    });
    document.getElementById('add-access').addEventListener('click', () => {
      const database = databaseChoice.value;
      if (!database || [...accessRows.children].some(row => row.dataset.access === database)) return;
      const row = accessRows.insertRow(); row.dataset.access = database;
      const databaseCell = cell(row, database);
      const hidden = document.createElement('input'); hidden.type = 'hidden'; hidden.name = 'access_database'; hidden.value = database; databaseCell.append(hidden);
      const select = document.createElement('select'); select.className = 'form-select'; select.name = 'access_permission'; select.setAttribute('aria-label', `Permission for ${database}`);
      for (const label of ['View Only', 'View + Edit']) { const option = document.createElement('option'); option.value = label; option.textContent = label; select.append(option); }
      row.insertCell().append(select); removeButton(row, 'access');
      databaseChoice.value = ''; updateEmpty();
    });
    groupForm.addEventListener('click', event => {
      const button = event.target.closest('[data-remove]');
      if (!button) return;
      const message = button.dataset.remove === 'access' ? 'Remove this database access from the group?' : 'Remove this member from the group?';
      if (window.confirm(message)) { button.closest('tr').remove(); updateEmpty(); }
    });
  }

  const connectionForm = document.getElementById('connection-form');
  if (connectionForm) {
    const result = document.getElementById('connection-result');
    document.getElementById('test-connection').addEventListener('click', async () => {
      if (!connectionForm.reportValidity()) return;
      result.hidden = false;
      result.textContent = 'Testing connection…';
      try {
        const response = await fetch('/app/admin/databases/test', {
          method: 'POST',
          credentials: 'same-origin',
          headers: {'Content-Type': 'application/x-www-form-urlencoded'},
          body: new URLSearchParams(new FormData(connectionForm))
        });
        if (!response.ok) throw new Error('connection test failed');
        result.textContent = 'Connection successful.';
      } catch (_error) {
        result.textContent = 'Connection test failed. Check the database details and try again.';
      }
    });
    connectionForm.addEventListener('input', () => { result.hidden = true; });
  }
})();
