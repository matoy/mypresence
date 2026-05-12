// Presence App - JavaScript
// Calendar drag-to-select + Admin AJAX helpers

// ============================================================
// Dark Mode Toggle (Alpine.js component)
// ============================================================
function darkModeToggle() {
    return {
        mode: localStorage.getItem('darkMode') || 'system',
        get icon() {
            if (this.mode === 'light') return '☀️';
            if (this.mode === 'dark') return '🌙';
            return '🖥️';
        },
        get label() {
            if (this.mode === 'light') return (_t['nav.theme.light']  || 'Light');
            if (this.mode === 'dark')  return (_t['nav.theme.dark']   || 'Dark');
            return (_t['nav.theme.system'] || 'System');
        },
        cycle() {
            const modes = ['system', 'light', 'dark'];
            const idx = modes.indexOf(this.mode);
            this.mode = modes[(idx + 1) % modes.length];
            localStorage.setItem('darkMode', this.mode);
            this.apply();
        },
        apply() {
            const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
            if (this.mode === 'dark' || (this.mode === 'system' && prefersDark)) {
                document.documentElement.classList.add('dark');
            } else {
                document.documentElement.classList.remove('dark');
            }
        },
        init() {
            // Listen for system preference changes when in 'system' mode
            window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
                if (this.mode === 'system') this.apply();
            });
        }
    };
}

// ============================================================
// Calendar Component (Alpine.js)
// ============================================================
function calendarApp(statuses, currentUserId, isAdmin, presences) {
    return {
        statuses: statuses || [],
        currentUserId: currentUserId,
        isAdmin: isAdmin,
        presences: presences || {},
        selecting: false,
        selectedUserId: null,
        selectedDates: [],
        startDate: null,
        showPicker: false,
        pickerX: 0,
        pickerY: 0,
        _selectButton: 0,          // mouse button that started the current selection
        _contextMenuViaMouseup: false, // flag to skip @contextmenu when mouseup already showed it
        // Half-day context menu state
        showContextMenu: false,
        contextMenuX: 0,
        contextMenuY: 0,
        contextMenuDate: null,
        contextMenuUserId: null,
        pendingHalf: 'full',
        longPressTimer: null,
        contextMenuForSelection: false,

        // Seat reservation modal state
        showSeatModal: false,
        seatFloorplans: [],
        seatFloorplanID: 0,
        seatFloorplanImage: '',
        seatModalSeats: [],
        seatModalLoading: false,
        selectedSeatID: null,

        // Check if a cell is blocked (weekend or non-imputable holiday)
        isCellBlocked(userId, date) {
            const cell = document.querySelector(`[data-user-id="${userId}"][data-date="${date}"]`);
            if (!cell) return false;
            if (cell.dataset.weekend === "true") return true;
            if (cell.dataset.holiday === "true" && cell.dataset.holidayAllowImputed !== "true") return true;
            return false;
        },

        // Start selection on mousedown/touchstart
        startSelect(userId, date, event) {
            // Only allow editing own presences (admin/manager can edit anyone)
            if (!this.isAdmin && userId !== this.currentUserId) return;
            if (this.isCellBlocked(userId, date)) return;

            // Remember which button initiated the drag
            this._selectButton = event ? event.button : 0;
            const isTouch = event && event.type === 'touchstart';

            // Long-press (600ms) opens the context menu — touch only.
            // Mouse interactions use mouseup instead, so the timer must not fire.
            if (this.longPressTimer) clearTimeout(this.longPressTimer);
            this.longPressTimer = isTouch ? setTimeout(() => {
                this.longPressTimer = null;
                this.selecting = false;
                this.selectedDates = [];
                if (navigator.vibrate) navigator.vibrate(30);
                const cell = document.querySelector(`[data-user-id="${userId}"][data-date="${date}"]`);
                const rect = cell ? cell.getBoundingClientRect() : { left: 16, bottom: 120, width: 36 };
                this.openContextMenu(userId, date, {
                    clientX: Math.min(rect.left, window.innerWidth - 220),
                    clientY: Math.min(rect.bottom + 4, window.innerHeight - 230)
                });
            }, 600) : null;

            this.selecting = true;
            this.selectedUserId = userId;
            this.selectedDates = [date];
            this.startDate = date;
            this.showPicker = false;
        },

        // Extend selection on mousemove/touchmove
        extendSelect(userId, date) {
            if (!this.selecting || userId !== this.selectedUserId) return;
            if (this.isCellBlocked(userId, date)) return;

            // Cancel long-press as soon as the user drags to a new cell
            if (date !== this.startDate && this.longPressTimer) {
                clearTimeout(this.longPressTimer);
                this.longPressTimer = null;
            }

            // Build date range between startDate and current date
            const start = new Date(this.startDate);
            const end = new Date(date);
            const minDate = start < end ? start : end;
            const maxDate = start < end ? end : start;

            this.selectedDates = [];
            const current = new Date(minDate);
            while (current <= maxDate) {
                const d = current.toISOString().split('T')[0];
                if (!this.isCellBlocked(userId, d)) {
                    this.selectedDates.push(d);
                }
                current.setDate(current.getDate() + 1);
            }
        },

        // Handle touch move for mobile
        handleTouchMove(event) {
            // Cancel long-press if the finger moves
            if (this.longPressTimer) {
                clearTimeout(this.longPressTimer);
                this.longPressTimer = null;
            }
            if (!this.selecting) return;
            const touch = event.touches[0];
            const element = document.elementFromPoint(touch.clientX, touch.clientY);
            if (element) {
                const cell = element.closest('[data-user-id][data-date]');
                if (cell) {
                    const userId = parseInt(cell.dataset.userId);
                    const date = cell.dataset.date;
                    this.extendSelect(userId, date);
                }
            }
        },

        // Check if a cell is in the current selection
        isSelected(userId, date) {
            return this.selecting && 
                   this.selectedUserId === userId && 
                   this.selectedDates.includes(date);
        },

        // Apply a status to selected dates
        async applyStatus(statusId) {
            if (!this.selectedDates.length || !this.selectedUserId) return;

            try {
                const resp = await fetch('/api/presences', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        user_id: this.selectedUserId,
                        dates: this.selectedDates,
                        status_id: statusId,
                        half: this.pendingHalf
                    })
                });
                if (resp.ok) {
                    window.location.reload();
                } else {
                    const data = await resp.json();
                    alert(data.error || 'Erreur');
                }
            } catch (e) {
                alert('Erreur de connexion');
            }
            this.pendingHalf = 'full';
            this.cancelSelect();
        },

        // Clear presences for selected dates
        async clearStatus() {
            if (!this.selectedDates.length || !this.selectedUserId) return;

            try {
                const resp = await fetch('/api/presences/clear', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        user_id: this.selectedUserId,
                        dates: this.selectedDates,
                        half: ''
                    })
                });
                if (resp.ok) {
                    window.location.reload();
                }
            } catch (e) {
                alert('Erreur de connexion');
            }
            this.pendingHalf = 'full';
            this.cancelSelect();
        },

        cancelSelect() {
            this.selecting = false;
            this.selectedDates = [];
            this.selectedUserId = null;
            this.showPicker = false;
            this.showContextMenu = false;
            this.pendingHalf = 'full';
            this.contextMenuForSelection = false;
        },

        // Open right-click context menu for half-day selection
        openContextMenu(userId, date, event) {
            if (!this.isAdmin && userId !== this.currentUserId) return;
            if (this.isCellBlocked(userId, date)) return;
            // Ignore contextmenu events fired during an active selection (e.g. the
            // browser fires contextmenu on right-mousedown, before the button is
            // released). The mouseup handler will show the menu at the right time.
            if (this.selecting) return;
            // mouseup already set up the context menu for right-click release
            if (this._contextMenuViaMouseup) {
                this._contextMenuViaMouseup = false;
                return;
            }
            this.contextMenuForSelection = false;
            this.showContextMenu = true;
            this.showPicker = false;
            this.contextMenuDate = date;
            this.contextMenuUserId = userId;
            this.contextMenuX = Math.min(event.clientX + 5, window.innerWidth - 220);
            this.contextMenuY = Math.min(event.clientY + 5, window.innerHeight - 210);
        },

        // Select half (AM / full / PM) and open status picker
        selectHalf(half) {
            this.pendingHalf = half;
            this.showContextMenu = false;
            if (this.contextMenuForSelection && this.selectedDates.length > 0) {
                // Preserve multi-cell drag selection
                this.selectedUserId = this.contextMenuUserId;
            } else {
                this.selectedUserId = this.contextMenuUserId;
                this.selectedDates = [this.contextMenuDate];
            }
            this.contextMenuForSelection = false;
            this.pickerX = this.contextMenuX;
            this.pickerY = this.contextMenuY;
            this.showPicker = true;
        },

        // Check whether a date has at least one presence declared
        hasPresence(date) {
            if (!date) return false;
            const halves = this.presences[date];
            if (!halves) return false;
            return !!(halves['full'] || halves['AM'] || halves['PM']);
        },

        // Return the primary statusId for a date (full > AM > PM)
        getDateStatusId(date) {
            const halves = this.presences[date];
            if (!halves) return null;
            return halves['full'] || halves['AM'] || halves['PM'] || null;
        },

        // Generate and download an .ics file for the given date
        addToCalendar(date) {
            this.showContextMenu = false;
            const statusId = this.getDateStatusId(date);
            if (!statusId) return;
            const status = this.statuses.find(s => s.id === statusId);
            if (!status) return;

            let busyStatus, transp;
            if (status.billable && status.on_site) {
                busyStatus = 'FREE';
                transp = 'TRANSPARENT';
            } else if (status.billable && !status.on_site) {
                busyStatus = 'WORKINGELSEWHERE';
                transp = 'OPAQUE';
            } else {
                busyStatus = 'OOF';
                transp = 'OPAQUE';
            }

            const dtstart = date.replace(/-/g, '');
            const nextDay = new Date(date + 'T00:00:00');
            nextDay.setDate(nextDay.getDate() + 1);
            const dtend = nextDay.toISOString().split('T')[0].replace(/-/g, '');
            const uid = (typeof crypto !== 'undefined' && crypto.randomUUID)
                ? crypto.randomUUID()
                : (Date.now() + Math.random()).toString(36);

            const ics = [
                'BEGIN:VCALENDAR',
                'VERSION:2.0',
                'CALSCALE:GREGORIAN',
                'PRODID:-//Presence App//FR',
                'BEGIN:VEVENT',
                'UID:' + uid + '@presence-app',
                'DTSTART;VALUE=DATE:' + dtstart,
                'DTEND;VALUE=DATE:' + dtend,
                'SUMMARY:' + status.name,
                'TRANSP:' + transp,
                'X-MICROSOFT-CDO-BUSYSTATUS:' + busyStatus,
                'X-MICROSOFT-CDO-ALLDAYEVENT:TRUE',
                'X-MICROSOFT-CDO-REMINDER-SET:FALSE',
                'END:VEVENT',
                'END:VCALENDAR'
            ].join('\r\n');

            const blob = new Blob([ics], { type: 'text/calendar;charset=utf-8' });
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = 'presence-' + date + '.ics';
            document.body.appendChild(a);
            a.click();
            document.body.removeChild(a);
            URL.revokeObjectURL(url);
        },

        // Returns the dates to use for bulk seat booking:
        // the active drag-selection if any, otherwise just the right-clicked date.
        getSeatBookingDates() {
            if (this.selectedDates.length > 0) {
                return this.selectedDates;
            }
            return this.contextMenuDate ? [this.contextMenuDate] : [];
        },

        // Open the seat picker modal, lazy-loading floorplans only.
        // Seats are loaded when the user picks a plan from the dropdown.
        async openSeatModal() {
            this.showContextMenu = false;
            this.showSeatModal = true;
            this.seatModalLoading = true;
            this.selectedSeatID = null;
            this.seatModalSeats = [];
            this.seatFloorplanID = 0;
            this.seatFloorplanImage = '';
            try {
                const resp = await fetch('/api/floorplans');
                if (resp.ok) {
                    this.seatFloorplans = await resp.json();
                    // Auto-select the first plan and load its seats
                    if (this.seatFloorplans.length > 0) {
                        this.seatFloorplanID = this.seatFloorplans[0].id;
                        await this.loadSeatModalSeats();
                    }
                }
            } catch (e) {
                // ignore - modal will show empty state
            } finally {
                this.seatModalLoading = false;
            }
        },

        // Fetch seats for the currently selected floorplan in the modal.
        async loadSeatModalSeats() {
            if (!this.seatFloorplanID) return;
            this.seatModalLoading = true;
            this.selectedSeatID = null;
            this.seatFloorplanImage = '';
            try {
                const fp = this.seatFloorplans.find(f => f.id === this.seatFloorplanID);
                if (fp) this.seatFloorplanImage = fp.image_path || '';
                const dates = this.getSeatBookingDates();
                const params = new URLSearchParams({ half: this.pendingHalf || 'full' });
                if (dates.length > 0) params.set('dates', dates.join(','));
                const resp = await fetch(`/api/floorplans/${this.seatFloorplanID}/seats/status?${params}`);
                if (resp.ok) {
                    this.seatModalSeats = await resp.json();
                }
            } finally {
                this.seatModalLoading = false;
            }
        },

        // Submit bulk seat reservation for the active selection.
        async bookSeatsForSelection() {
            if (!this.selectedSeatID) return;
            const dates = this.getSeatBookingDates();
            if (!dates.length) return;
            this.showSeatModal = false;
            try {
                await fetch('/api/reservations/bulk', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        seat_id: this.selectedSeatID,
                        dates: dates,
                        half: this.pendingHalf
                    })
                });
            } catch (e) { /* ignore */ }
            window.location.reload();
        },

        // Cancel all seat reservations for the active selection.
        async cancelSeatsForSelection() {
            this.showContextMenu = false;
            const dates = this.getSeatBookingDates();
            if (!dates.length) return;
            try {
                await fetch('/api/reservations/bulk', {
                    method: 'DELETE',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ dates })
                });
            } catch (e) { /* ignore */ }
            window.location.reload();
        },

        // Clear all halves for the context menu target date
        async clearDay() {
            this.showContextMenu = false;
            if (!this.contextMenuUserId || !this.contextMenuDate) return;
            try {
                const resp = await fetch('/api/presences/clear', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        user_id: this.contextMenuUserId,
                        dates: [this.contextMenuDate],
                        half: ''
                    })
                });
                if (resp.ok) {
                    window.location.reload();
                } else {
                    const d = await resp.json();
                    alert(d.error || 'Erreur');
                }
            } catch (e) {
                alert('Erreur de connexion');
            }
        },

        // Initialize event listeners
        init() {
            // End selection on mouseup
            document.addEventListener('mouseup', (e) => {
                if (this.longPressTimer) {
                    clearTimeout(this.longPressTimer);
                    this.longPressTimer = null;
                }
                if (this.selecting && this.selectedDates.length > 0) {
                    this.selecting = false;

                    if (e.button === 2) {
                        // Right-click release: show AM/PM context menu for the selection
                        this._contextMenuViaMouseup = true;
                        this.contextMenuForSelection = this.selectedDates.length > 1;
                        this.contextMenuUserId = this.selectedUserId;
                        this.contextMenuDate = this.selectedDates[this.selectedDates.length - 1];
                        this.contextMenuX = Math.min(e.clientX + 5, window.innerWidth - 220);
                        this.contextMenuY = Math.min(e.clientY + 5, window.innerHeight - 210);
                        this.showContextMenu = true;
                        this.showPicker = false;
                    } else {
                        // Left-click release: show status picker directly
                        this.showPicker = true;
                        this.pickerX = Math.min(e.clientX + 10, window.innerWidth - 280);
                        this.pickerY = Math.min(e.clientY + 10, window.innerHeight - 400);
                    }
                }
            });

            // End selection on touchend
            document.addEventListener('touchend', (e) => {
                if (this.longPressTimer) {
                    clearTimeout(this.longPressTimer);
                    this.longPressTimer = null;
                }
                if (this.selecting && this.selectedDates.length > 0) {
                    const touch = e.changedTouches[0];
                    const x = Math.min(touch.clientX + 10, window.innerWidth - 220);
                    const y = Math.max(10, Math.min(touch.clientY - 10, window.innerHeight - 300));

                    if (this.selectedDates.length > 1) {
                        // Multi-cell drag: show context menu to choose AM/PM/full first
                        this.contextMenuForSelection = true;
                        this.contextMenuUserId = this.selectedUserId;
                        this.contextMenuDate = this.selectedDates[this.selectedDates.length - 1];
                        this.contextMenuX = x;
                        this.contextMenuY = y;
                        this.showContextMenu = true;
                        this.showPicker = false;
                    } else {
                        // Single cell: show picker directly
                        this.showPicker = true;
                        this.pickerX = Math.min(touch.clientX + 10, window.innerWidth - 280);
                        this.pickerY = Math.min(touch.clientY - 200, window.innerHeight - 400);
                        if (this.pickerY < 10) this.pickerY = 10;
                    }

                    this.selecting = false;
                }
            });

            // Close picker on Escape
            document.addEventListener('keydown', (e) => {
                if (e.key === 'Escape') {
                    this.cancelSelect();
                }
            });
        }
    };
}

// ============================================================
// Team Calendar Component (Alpine.js)
// Handles a multi-row presence table for team members.
// canEdit=true for team leaders / managers / global admins.
// allPresences: { userId: { date: { half: statusId } } }
// ============================================================
function teamCalendarApp(statuses, currentUserId, canEdit, allPresences) {
    return {
        statuses: statuses || [],
        currentUserId,
        canEdit,
        allPresences: allPresences || {},

        selecting: false,
        selectedUserId: null,
        selectedDates: [],
        startDate: null,
        showPicker: false,
        pickerX: 0,
        pickerY: 0,
        _selectButton: 0,
        _contextMenuViaMouseup: false,
        showContextMenu: false,
        contextMenuX: 0,
        contextMenuY: 0,
        contextMenuDate: null,
        contextMenuUserId: null,
        pendingHalf: 'full',
        longPressTimer: null,
        contextMenuForSelection: false,

        // Seat reservation modal state
        showSeatModal: false,
        seatFloorplans: [],
        seatFloorplanID: 0,
        seatFloorplanImage: '',
        seatModalSeats: [],
        seatModalLoading: false,
        selectedSeatID: null,

        isCellBlocked(userId, date) {
            // Scope query to this Alpine root element to avoid cross-table conflicts.
            const root = this.$el || document;
            const cell = root.querySelector(`[data-user-id="${userId}"][data-date="${date}"]`);
            if (!cell) return false;
            if (cell.dataset.weekend === 'true') return true;
            if (cell.dataset.holiday === 'true' && cell.dataset.holidayAllowImputed !== 'true') return true;
            return false;
        },

        startSelect(userId, date, event) {
            if (!this.canEdit) return;
            if (this.isCellBlocked(userId, date)) return;

            this._selectButton = event ? event.button : 0;
            const isTouch = event && event.type === 'touchstart';

            if (this.longPressTimer) clearTimeout(this.longPressTimer);
            this.longPressTimer = isTouch ? setTimeout(() => {
                this.longPressTimer = null;
                this.selecting = false;
                this.selectedDates = [];
                if (navigator.vibrate) navigator.vibrate(30);
                const root = this.$el || document;
                const cell = root.querySelector(`[data-user-id="${userId}"][data-date="${date}"]`);
                const rect = cell ? cell.getBoundingClientRect() : { left: 16, bottom: 120 };
                this.openContextMenu(userId, date, {
                    clientX: Math.min(rect.left, window.innerWidth - 220),
                    clientY: Math.min(rect.bottom + 4, window.innerHeight - 230)
                });
            }, 600) : null;

            this.selecting = true;
            this.selectedUserId = userId;
            this.selectedDates = [date];
            this.startDate = date;
            this.showPicker = false;
        },

        extendSelect(userId, date) {
            if (!this.selecting || userId !== this.selectedUserId) return;
            if (this.isCellBlocked(userId, date)) return;
            if (date !== this.startDate && this.longPressTimer) {
                clearTimeout(this.longPressTimer);
                this.longPressTimer = null;
            }
            const start = new Date(this.startDate);
            const end = new Date(date);
            const minDate = start < end ? start : end;
            const maxDate = start < end ? end : start;
            this.selectedDates = [];
            const current = new Date(minDate);
            while (current <= maxDate) {
                const d = current.toISOString().split('T')[0];
                if (!this.isCellBlocked(userId, d)) this.selectedDates.push(d);
                current.setDate(current.getDate() + 1);
            }
        },

        handleTouchMove(event) {
            if (this.longPressTimer) { clearTimeout(this.longPressTimer); this.longPressTimer = null; }
            if (!this.selecting) return;
            const touch = event.touches[0];
            const element = document.elementFromPoint(touch.clientX, touch.clientY);
            if (element) {
                const cell = element.closest('[data-user-id][data-date]');
                if (cell) this.extendSelect(parseInt(cell.dataset.userId), cell.dataset.date);
            }
        },

        isSelected(userId, date) {
            return this.selecting && this.selectedUserId === userId && this.selectedDates.includes(date);
        },

        async applyStatus(statusId) {
            if (!this.selectedDates.length || !this.selectedUserId) return;
            try {
                const resp = await fetch('/api/presences', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ user_id: this.selectedUserId, dates: this.selectedDates, status_id: statusId, half: this.pendingHalf })
                });
                if (resp.ok) { window.location.reload(); }
                else { const d = await resp.json(); alert(d.error || 'Erreur'); }
            } catch (e) { alert('Erreur de connexion'); }
            this.pendingHalf = 'full';
            this.cancelSelect();
        },

        async clearStatus() {
            if (!this.selectedDates.length || !this.selectedUserId) return;
            try {
                const resp = await fetch('/api/presences/clear', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ user_id: this.selectedUserId, dates: this.selectedDates, half: '' })
                });
                if (resp.ok) window.location.reload();
            } catch (e) { alert('Erreur de connexion'); }
            this.pendingHalf = 'full';
            this.cancelSelect();
        },

        // Returns dates for seat booking: active selection or just the right-clicked date.
        getSeatBookingDates() {
            if (this.selectedDates.length > 0) return this.selectedDates;
            return this.contextMenuDate ? [this.contextMenuDate] : [];
        },

        // Returns the target user ID for seat operations.
        getSeatBookingUserId() {
            return this.contextMenuUserId || this.selectedUserId;
        },

        async openSeatModal() {
            this.showContextMenu = false;
            this.showSeatModal = true;
            this.seatModalLoading = true;
            this.selectedSeatID = null;
            this.seatModalSeats = [];
            this.seatFloorplanID = 0;
            this.seatFloorplanImage = '';
            try {
                const resp = await fetch('/api/floorplans');
                if (resp.ok) {
                    this.seatFloorplans = await resp.json();
                    if (this.seatFloorplans.length > 0) {
                        this.seatFloorplanID = this.seatFloorplans[0].id;
                        await this.loadSeatModalSeats();
                    }
                }
            } catch (e) { /* ignore */ } finally {
                this.seatModalLoading = false;
            }
        },

        async loadSeatModalSeats() {
            if (!this.seatFloorplanID) return;
            this.seatModalLoading = true;
            this.selectedSeatID = null;
            this.seatFloorplanImage = '';
            try {
                const fp = this.seatFloorplans.find(f => f.id === this.seatFloorplanID);
                if (fp) this.seatFloorplanImage = fp.image_path || '';
                const dates = this.getSeatBookingDates();
                const params = new URLSearchParams({ half: this.pendingHalf || 'full' });
                if (dates.length > 0) params.set('dates', dates.join(','));
                const resp = await fetch(`/api/floorplans/${this.seatFloorplanID}/seats/status?${params}`);
                if (resp.ok) this.seatModalSeats = await resp.json();
            } finally {
                this.seatModalLoading = false;
            }
        },

        async bookSeatsForSelection() {
            if (!this.selectedSeatID) return;
            const dates = this.getSeatBookingDates();
            const userId = this.getSeatBookingUserId();
            if (!dates.length || !userId) return;
            this.showSeatModal = false;
            try {
                await fetch('/api/reservations/bulk', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ seat_id: this.selectedSeatID, dates, half: this.pendingHalf, user_id: userId })
                });
            } catch (e) { /* ignore */ }
            window.location.reload();
        },

        async cancelSeatsForSelection() {
            this.showContextMenu = false;
            const dates = this.getSeatBookingDates();
            const userId = this.getSeatBookingUserId();
            if (!dates.length || !userId) return;
            try {
                await fetch('/api/reservations/bulk', {
                    method: 'DELETE',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ dates, user_id: userId })
                });
            } catch (e) { /* ignore */ }
            window.location.reload();
        },

        async clearDay() {
            this.showContextMenu = false;
            if (!this.contextMenuUserId || !this.contextMenuDate) return;
            try {
                const resp = await fetch('/api/presences/clear', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ user_id: this.contextMenuUserId, dates: [this.contextMenuDate], half: '' })
                });
                if (resp.ok) { window.location.reload(); }
                else { const d = await resp.json(); alert(d.error || 'Erreur'); }
            } catch (e) { alert('Erreur de connexion'); }
        },

        cancelSelect() {
            this.selecting = false;
            this.selectedDates = [];
            this.selectedUserId = null;
            this.showPicker = false;
            this.showContextMenu = false;
            this.pendingHalf = 'full';
            this.contextMenuForSelection = false;
        },

        openContextMenu(userId, date, event) {
            if (!this.canEdit) return;
            if (this.isCellBlocked(userId, date)) return;
            if (this.selecting) return;
            if (this._contextMenuViaMouseup) { this._contextMenuViaMouseup = false; return; }
            this.contextMenuForSelection = false;
            this.showContextMenu = true;
            this.showPicker = false;
            this.contextMenuDate = date;
            this.contextMenuUserId = userId;
            this.contextMenuX = Math.min(event.clientX + 5, window.innerWidth - 220);
            this.contextMenuY = Math.min(event.clientY + 5, window.innerHeight - 210);
        },

        selectHalf(half) {
            this.pendingHalf = half;
            this.showContextMenu = false;
            this.selectedUserId = this.contextMenuUserId;
            if (!this.contextMenuForSelection || this.selectedDates.length === 0) {
                this.selectedDates = [this.contextMenuDate];
            }
            this.contextMenuForSelection = false;
            this.pickerX = this.contextMenuX;
            this.pickerY = this.contextMenuY;
            this.showPicker = true;
        },

        init() {
            document.addEventListener('mouseup', (e) => {
                if (this.longPressTimer) { clearTimeout(this.longPressTimer); this.longPressTimer = null; }
                if (this.selecting && this.selectedDates.length > 0) {
                    this.selecting = false;
                    if (e.button === 2) {
                        this._contextMenuViaMouseup = true;
                        this.contextMenuForSelection = this.selectedDates.length > 1;
                        this.contextMenuUserId = this.selectedUserId;
                        this.contextMenuDate = this.selectedDates[this.selectedDates.length - 1];
                        this.contextMenuX = Math.min(e.clientX + 5, window.innerWidth - 220);
                        this.contextMenuY = Math.min(e.clientY + 5, window.innerHeight - 210);
                        this.showContextMenu = true;
                        this.showPicker = false;
                    } else {
                        this.showPicker = true;
                        this.pickerX = Math.min(e.clientX + 10, window.innerWidth - 280);
                        this.pickerY = Math.min(e.clientY + 10, window.innerHeight - 400);
                    }
                }
            });
            document.addEventListener('touchend', (e) => {
                if (this.longPressTimer) { clearTimeout(this.longPressTimer); this.longPressTimer = null; }
                if (this.selecting && this.selectedDates.length > 0) {
                    const touch = e.changedTouches[0];
                    const x = Math.min(touch.clientX + 10, window.innerWidth - 220);
                    const y = Math.max(10, Math.min(touch.clientY - 10, window.innerHeight - 300));
                    if (this.selectedDates.length > 1) {
                        this.contextMenuForSelection = true;
                        this.contextMenuUserId = this.selectedUserId;
                        this.contextMenuDate = this.selectedDates[this.selectedDates.length - 1];
                        this.contextMenuX = x; this.contextMenuY = y;
                        this.showContextMenu = true; this.showPicker = false;
                    } else {
                        this.showPicker = true;
                        this.pickerX = Math.min(touch.clientX + 10, window.innerWidth - 280);
                        this.pickerY = Math.min(touch.clientY - 200, window.innerHeight - 400);
                        if (this.pickerY < 10) this.pickerY = 10;
                    }
                    this.selecting = false;
                }
            });
            document.addEventListener('keydown', (e) => {
                if (e.key === 'Escape') this.cancelSelect();
            });
        }
    };
}

// ============================================================
// Admin: Teams management
// ============================================================
function teamsAdmin(initialTeams) {
    return {
        teams: initialTeams || [],
        newTeamName: '',
        createError: '',
        showCreateModal: false,
        filterText: '',
        filterMembers: 'all',

        get totalCount() {
            return this.teams.length;
        },

        get filteredCount() {
            return this.teams.filter(t => {
                const activeMemberCount = (t.Members || []).filter(m => !m.left_at).length;
                return this.matchesTeam((t.Team && t.Team.name) || '', activeMemberCount);
            }).length;
        },

        resetFilters() {
            this.filterText = '';
            this.filterMembers = 'all';
        },

        matchesTeam(name, memberCount) {
            const q = this.filterText.trim().toLowerCase();
            if (q && !(name || '').toLowerCase().includes(q)) return false;
            if (this.filterMembers === 'with' && memberCount <= 0) return false;
            if (this.filterMembers === 'empty' && memberCount > 0) return false;
            return true;
        },

        async createTeam() {
            this.createError = '';
            if (!this.newTeamName.trim()) {
                this.createError = 'Name is required';
                return;
            }
            const r = await fetch('/admin/teams', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: this.newTeamName.trim() })
            });
            if (r.ok) {
                this.showCreateModal = false;
                window.location.reload();
                return;
            }
            try {
                const d = await r.json();
                this.createError = d.error || 'Error';
            } catch (e) {
                this.createError = 'Error';
            }
        },

        async renameTeam(id, name) {
            await fetch(`/admin/teams/${id}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name })
            });
            window.location.reload();
        },

        async deleteTeam(id) {
            await fetch(`/admin/teams/${id}`, { method: 'DELETE' });
            window.location.reload();
        },

        async addMember(teamId, userId) {
            await fetch(`/admin/teams/${teamId}/members`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ user_id: parseInt(userId) })
            });
            window.location.reload();
        },

        async removeMember(teamId, userId) {
            if (!confirm('Remove this member from the team?')) return;
            await fetch(`/admin/teams/${teamId}/members/${userId}`, { method: 'DELETE' });
            window.location.reload();
        },

        async setLeftAt(teamId, userId, leftAt) {
            await fetch(`/admin/teams/${teamId}/members/${userId}/left-at`, {
                method: 'PATCH',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ left_at: leftAt })
            });
            window.location.reload();
        },

        async clearLeftAt(teamId, userId) {
            await fetch(`/admin/teams/${teamId}/members/${userId}/left-at`, {
                method: 'PATCH',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ left_at: null })
            });
            window.location.reload();
        }
    };
}

// ============================================================
// Admin: Status management
// ============================================================
function statusAdmin(initialStatuses) {
    return {
        statuses: initialStatuses || [],
        newName: '',
        newColor: '#3b82f6',
        newOrder: 0,
        newBillable: false,
        newOnSite: false,
        createError: '',
        showCreateModal: false,
        filterText: '',
        filterBillable: 'all',
        filterOnSite: 'all',
        filterDisabled: 'all',

        get totalCount() {
            return this.statuses.length;
        },

        get filteredCount() {
            return this.statuses.filter(s => this.matchesStatus(s.name, s.billable, s.on_site, s.disabled)).length;
        },

        resetFilters() {
            this.filterText = '';
            this.filterBillable = 'all';
            this.filterOnSite = 'all';
            this.filterDisabled = 'all';
        },

        openCreateModal() {
            this.newName = '';
            this.newColor = '#3b82f6';
            this.newOrder = 0;
            this.newBillable = false;
            this.newOnSite = false;
            this.createError = '';
            this.showCreateModal = true;
        },

        matchesStatus(name, billable, onSite, disabled) {
            const q = this.filterText.trim().toLowerCase();
            if (q && !(name || '').toLowerCase().includes(q)) return false;
            if (this.filterBillable === '1' && !billable) return false;
            if (this.filterBillable === '0' && billable) return false;
            if (this.filterOnSite === '1' && !onSite) return false;
            if (this.filterOnSite === '0' && onSite) return false;
            if (this.filterDisabled === '0' && disabled) return false;
            if (this.filterDisabled === '1' && !disabled) return false;
            return true;
        },

        async createStatus() {
            this.createError = '';
            if (!this.newName || !this.newColor) { this.createError = 'Name and color are required'; return; }
            const resp = await fetch('/admin/statuses', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: this.newName, color: this.newColor, sort_order: this.newOrder, billable: this.newBillable, on_site: this.newOnSite })
            });
            if (resp.ok) { this.showCreateModal = false; window.location.reload(); }
            else { const d = await resp.json(); this.createError = d.error || 'Error'; }
        },

        async updateStatus(id, name, color, billable, onSite, sortOrder) {
            await fetch(`/admin/statuses/${id}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name, color, billable, on_site: onSite, sort_order: sortOrder })
            });
            window.location.reload();
        },

        async deleteStatus(id) {
            const resp = await fetch(`/admin/statuses/${id}`, { method: 'DELETE' });
            if (!resp.ok) {
                const d = await resp.json();
                const msg = _t[d.error] || d.error || 'Error';
                alert(msg);
                return;
            }
            window.location.reload();
        },

        async toggleDisabled(id, disabled) {
            await fetch(`/admin/statuses/${id}/disabled`, {
                method: 'PATCH',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ disabled })
            });
            window.location.reload();
        }
    };
}
