/* Shared select-only combobox. Option data stays compatible with feed settings. */
(() => {
    let nextID = 0;
    class PodsyncSelect extends HTMLElement {
        static formAssociated = true;
        static observedAttributes = ['disabled', 'required', 'aria-label', 'aria-describedby', 'aria-invalid'];

        constructor() {
            super();
            this.internals = this.attachInternals();
            this.activeIndex = -1;
            this.opened = false;
            this.search = '';
            this.observer = new MutationObserver(records => {
                if (records.some(record => record.target === this || (record.target.nodeType === Node.ELEMENT_NODE ? record.target : record.target.parentElement)?.closest('option'))) this.render();
            });
            this.onOutside = event => { if (!this.contains(event.target)) this.close(); };
            this.onViewport = event => { if (!(event.target instanceof Node) || !this.list?.contains(event.target)) this.close(); };
            this.addEventListener('click', event => { if (event.target === this) this.focus(); });
        }

        connectedCallback() {
            this.classList.add('podsync-select');
            this.render();
            this.observer.observe(this, {childList: true, subtree: true, characterData: true, attributes: true, attributeFilter: ['disabled', 'selected', 'value', 'label']});
            document.addEventListener('pointerdown', this.onOutside);
            window.addEventListener('resize', this.onViewport);
            window.addEventListener('scroll', this.onViewport, true);
        }

        disconnectedCallback() {
            this.close();
            this.observer.disconnect();
            document.removeEventListener('pointerdown', this.onOutside);
            window.removeEventListener('resize', this.onViewport);
            window.removeEventListener('scroll', this.onViewport, true);
            clearTimeout(this.searchTimer);
        }

        attributeChangedCallback() { if (this.trigger) this.render(); }
        get options() { return Array.from(this.querySelectorAll(':scope > option')); }
        get selectedIndex() {
            const options = this.options;
            const selected = options.findIndex(option => option.selected);
            return selected >= 0 ? selected : this.emptySelection ? -1 : options.findIndex(option => !option.disabled);
        }
        set selectedIndex(index) {
            this.options.forEach((option, i) => { option.selected = i === Number(index); });
            this.emptySelection = Number(index) < 0 || Number(index) >= this.options.length;
            this.render();
        }
        get value() { return this.options[this.selectedIndex]?.value ?? ''; }
        set value(value) {
            this.selectedIndex = this.options.findIndex(option => option.value === String(value));
        }
        get disabled() { return this.hasAttribute('disabled'); }
        set disabled(value) { this.toggleAttribute('disabled', Boolean(value)); }
        get required() { return this.hasAttribute('required'); }
        set required(value) { this.toggleAttribute('required', Boolean(value)); }
        get labels() { return this.internals.labels; }
        focus(options) { this.trigger?.focus(options); }
        checkValidity() { return this.internals.checkValidity(); }
        reportValidity() { return this.internals.reportValidity(); }
        formDisabledCallback(disabled) { this.formDisabled = disabled; this.render(); }
        formResetCallback() {
            const index = this.options.findIndex(option => option.defaultSelected);
            this.selectedIndex = index >= 0 ? index : 0;
        }
        formStateRestoreCallback(value) { this.value = value; }

        render() {
            if (!this.isConnected) return;
            if (!this.trigger || !this.contains(this.trigger)) {
                this.opened = false;
                const uid = 'podsync-select-' + (++nextID);
                this.trigger = document.createElement('button');
                this.trigger.type = 'button';
                this.trigger.className = 'select-trigger';
                this.trigger.setAttribute('role', 'combobox');
                this.trigger.setAttribute('aria-haspopup', 'listbox');
                this.trigger.setAttribute('aria-controls', uid + '-list');
                this.trigger.innerHTML = '<span class="visually-hidden select-name"></span><span class="select-value"></span><i class="bi bi-chevron-down" aria-hidden="true"></i>';
                this.nameElement = this.trigger.querySelector('.select-name');
                this.nameElement.id = uid + '-name';
                this.valueElement = this.trigger.querySelector('.select-value');
                this.valueElement.id = uid + '-value';
                this.trigger.setAttribute('aria-labelledby', this.nameElement.id + ' ' + this.valueElement.id);
                this.list = document.createElement('div');
                this.list.id = uid + '-list';
                this.list.className = 'select-options';
                this.list.setAttribute('role', 'listbox');
                this.list.setAttribute('popover', 'manual');
                this.list.setAttribute('aria-labelledby', this.nameElement.id);
                this.append(this.trigger, this.list);
                this.trigger.addEventListener('click', () => this.opened ? this.close() : this.open());
                this.trigger.addEventListener('keydown', event => this.onKey(event));
                this.trigger.addEventListener('blur', () => this.close());
                // Retain focus on the combobox while an option is clicked.
                this.list.addEventListener('pointerdown', event => event.preventDefault());
                this.list.addEventListener('click', event => {
                    const option = event.target.closest('[role="option"]');
                    if (option && option.getAttribute('aria-disabled') !== 'true') this.commit(Number(option.dataset.index));
                });
                this.addEventListener('invalid', () => this.focus());
            }
            const options = this.options;
            this.nameElement.textContent = this.getAttribute('aria-label') || Array.from(this.labels || []).map(label => label.textContent.trim()).join(' ') || 'Choose an option';
            this.valueElement.textContent = options[this.selectedIndex]?.textContent || 'Choose an option';
            this.trigger.disabled = this.disabled || Boolean(this.formDisabled);
            this.trigger.setAttribute('aria-expanded', String(this.opened));
            this.trigger.setAttribute('aria-required', String(this.required));
            for (const name of ['aria-describedby', 'aria-invalid']) {
                const value = this.getAttribute(name);
                if (value) this.trigger.setAttribute(name, value);
                else this.trigger.removeAttribute(name);
            }
            this.list.replaceChildren(...options.map((option, index) => {
                const item = document.createElement('div');
                item.id = this.list.id + '-' + index;
                item.className = 'select-option';
                item.setAttribute('role', 'option');
                item.setAttribute('aria-selected', String(index === this.selectedIndex));
                item.setAttribute('aria-disabled', String(option.disabled));
                item.dataset.index = index;
                item.textContent = option.textContent;
                return item;
            }));
            this.internals.setFormValue(this.value);
            this.internals.setValidity(this.required && !this.value ? {valueMissing: true} : {}, this.required && !this.value ? 'Please choose an option.' : '', this.trigger);
            if (this.trigger.disabled) this.close();
            else if (this.opened) this.highlight(this.activeIndex);
        }

        open() {
            if (this.trigger.disabled || !this.options.length) return;
            this.opened = true;
            this.list.style.width = 'max-content';
            this.list.showPopover();
            const rect = this.trigger.getBoundingClientRect();
            const gap = parseFloat(getComputedStyle(this).getPropertyValue('--control-gap'));
            const max = parseFloat(getComputedStyle(this).getPropertyValue('--select-popup-max-height'));
            const width = Math.min(Math.max(rect.width, this.list.getBoundingClientRect().width), window.innerWidth - gap * 2);
            this.list.style.width = width + 'px';
            const below = window.innerHeight - rect.bottom - gap;
            const above = rect.top - gap;
            const useAbove = below < Math.min(max, this.list.scrollHeight) && above > below;
            const height = Math.min(max, useAbove ? above : below);
            Object.assign(this.list.style, {
                left: Math.max(gap, Math.min(rect.left, window.innerWidth - width - gap)) + 'px',
                top: (useAbove ? Math.max(gap, rect.top - Math.min(height, this.list.scrollHeight) - gap) : rect.bottom + gap) + 'px',
                width: width + 'px',
                maxHeight: Math.max(0, height) + 'px'
            });
            this.trigger.setAttribute('aria-expanded', 'true');
            const index = this.options[this.selectedIndex]?.disabled ? this.options.findIndex(option => !option.disabled) : this.selectedIndex;
            this.highlight(index >= 0 ? index : this.options.findIndex(option => !option.disabled));
        }

        close() {
            this.opened = false;
            if (this.list?.matches(':popover-open')) this.list.hidePopover();
            this.trigger?.setAttribute('aria-expanded', 'false');
            this.trigger?.removeAttribute('aria-activedescendant');
        }

        highlight(index) {
            this.activeIndex = index;
            for (const [i, item] of Array.from(this.list.children).entries()) item.classList.toggle('is-active', i === index);
            const active = this.list.children[index];
            if (active) {
                this.trigger.setAttribute('aria-activedescendant', active.id);
                const top = active.offsetTop;
                const bottom = top + active.offsetHeight;
                if (top < this.list.scrollTop) this.list.scrollTop = top;
                else if (bottom > this.list.scrollTop + this.list.clientHeight) this.list.scrollTop = bottom - this.list.clientHeight;
            } else this.trigger.removeAttribute('aria-activedescendant');
        }

        commit(index) {
            const previous = this.value;
            this.selectedIndex = index;
            this.close();
            this.focus();
            if (this.value !== previous) {
                this.dispatchEvent(new Event('input', {bubbles: true}));
                this.dispatchEvent(new Event('change', {bubbles: true}));
            }
        }

        onKey(event) {
            const options = this.options;
            const enabled = options.map((option, i) => option.disabled ? -1 : i).filter(i => i >= 0);
            if (event.key === 'Tab') { this.close(); return; }
            if (event.key === 'Escape' && this.opened) {
                event.preventDefault(); event.stopPropagation(); this.close(); return;
            }
            if (['Enter', ' ', 'ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
                event.preventDefault();
                if (!this.opened) {
                    this.open();
                    if (event.key === 'Home') this.highlight(enabled[0] ?? -1);
                    if (event.key === 'End') this.highlight(enabled[enabled.length - 1] ?? -1);
                    return;
                }
                if (event.key === 'Enter' || event.key === ' ') { if (this.activeIndex >= 0) this.commit(this.activeIndex); return; }
                const current = enabled.indexOf(this.activeIndex);
                const next = event.key === 'Home' ? enabled[0] : event.key === 'End' ? enabled[enabled.length - 1] : enabled[Math.max(0, Math.min(enabled.length - 1, current + (event.key === 'ArrowDown' ? 1 : -1)))];
                this.highlight(next ?? -1);
            } else if (event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) {
                event.preventDefault();
                this.search += event.key.toLocaleLowerCase();
                clearTimeout(this.searchTimer);
                this.searchTimer = setTimeout(() => { this.search = ''; }, 700);
                if (!this.opened) this.open();
                const index = enabled.find(i => options[i].textContent.trim().toLocaleLowerCase().startsWith(this.search));
                if (index !== undefined) this.highlight(index);
            }
        }
    }
    customElements.define('podsync-select', PodsyncSelect);
})();
