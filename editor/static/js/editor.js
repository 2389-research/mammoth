// ABOUTME: Handles keyboard shortcuts, htmx event handling, and editor interactions
// ABOUTME: for the mammoth-dot-editor. Provides undo/redo/validate/export shortcuts.

(function() {
    'use strict';

    // Keyboard shortcuts
    document.addEventListener('keydown', (e) => {
        // Ctrl+Z or Cmd+Z: Undo
        if ((e.ctrlKey || e.metaKey) && e.key === 'z' && !e.shiftKey) {
            e.preventDefault();
            const undoBtn = document.getElementById('undo-btn');
            if (undoBtn) {
                undoBtn.click();
            }
        }

        // Ctrl+Shift+Z or Cmd+Shift+Z: Redo
        if ((e.ctrlKey || e.metaKey) && e.key === 'z' && e.shiftKey) {
            e.preventDefault();
            const redoBtn = document.getElementById('redo-btn');
            if (redoBtn) {
                redoBtn.click();
            }
        }

        // Ctrl+S or Cmd+S: Export
        if ((e.ctrlKey || e.metaKey) && e.key === 's') {
            e.preventDefault();
            const exportBtn = document.querySelector('.export-btn');
            if (exportBtn) {
                window.location.href = exportBtn.href;
            }
        }

        // Ctrl+Enter or Cmd+Enter: Validate
        if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
            e.preventDefault();
            const validateBtn = document.getElementById('validate-btn');
            if (validateBtn) {
                validateBtn.click();
            }
        }
    });

    // After htmx swaps content, re-render the graph
    document.body.addEventListener('htmx:afterSwap', (e) => {
        // Re-render graph if it was updated
        if (e.detail.target.id === 'editor-panels' ||
            e.detail.target.id === 'graph-viewer' ||
            e.detail.target.closest('#editor-panels')) {

            // Trigger graph re-render
            if (window.renderGraph) {
                window.renderGraph();
            }
        }
    });

    // After htmx settles (animations complete), ensure graph is rendered
    document.body.addEventListener('htmx:afterSettle', (e) => {
        if (e.detail.target.id === 'editor-panels' ||
            e.detail.target.id === 'graph-viewer') {

            if (window.renderGraph) {
                window.renderGraph();
            }
        }
    });

    // Handle htmx errors
    document.body.addEventListener('htmx:responseError', (e) => {
        console.error('htmx error:', e.detail);
        alert('Request failed: ' + (e.detail.xhr.statusText || 'Unknown error'));
    });

    // Add click handlers to diagnostic items to highlight elements
    document.body.addEventListener('click', (e) => {
        const diagnostic = e.target.closest('.diagnostic');
        if (diagnostic) {
            const nodeId = diagnostic.dataset.nodeId;
            const edgeId = diagnostic.dataset.edgeId;

            if (nodeId && window.highlightNode) {
                window.highlightNode(nodeId);
            } else if (edgeId && window.highlightEdge) {
                window.highlightEdge(edgeId);
            }
        }
    });

    // Before form submission, convert new_attr_key/value to attr_ prefixed input
    // so the server's extractAttrs helper picks it up as a proper attribute
    document.body.addEventListener('htmx:configRequest', (e) => {
        const form = e.detail.elt;
        if (!form || !form.closest) return;

        const keyInput = form.querySelector('[name="new_attr_key"]') ||
                         form.querySelector('input[name="new_attr_key"]');
        const valInput = form.querySelector('[name="new_attr_value"]') ||
                         form.querySelector('input[name="new_attr_value"]');

        if (keyInput && valInput && keyInput.value.trim()) {
            e.detail.parameters['attr_' + keyInput.value.trim()] = valInput.value;
            delete e.detail.parameters['new_attr_key'];
            delete e.detail.parameters['new_attr_value'];
        }
    });

    console.log('Editor.js initialized');
})();
