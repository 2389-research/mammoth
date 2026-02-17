// ABOUTME: Landing page animations — teletype effect, scroll reveals, and simulated telemetry output.
// ABOUTME: Vanilla JS, no dependencies. Used by the Cold Launch landing page template.

(function() {
    'use strict';

    // -----------------------------------------------------------------------
    // Teletype Animation
    // Types text into an element character by character, like a teletype
    // printer. Returns a Promise that resolves when typing is complete.
    // After finishing, appends a blinking cursor span.
    // -----------------------------------------------------------------------

    function teletype(element, text, speed) {
        if (speed === undefined) {
            speed = 40;
        }

        return new Promise(function(resolve) {
            var index = 0;

            function typeNext() {
                if (index < text.length) {
                    element.textContent += text.charAt(index);
                    index++;
                    setTimeout(typeNext, speed);
                } else {
                    // Append blinking cursor after typing completes.
                    var cursor = document.createElement('span');
                    cursor.className = 'cl-cursor';
                    cursor.textContent = '\u258C'; // ▌
                    element.appendChild(cursor);
                    resolve();
                }
            }

            typeNext();
        });
    }

    // -----------------------------------------------------------------------
    // Scroll Reveal (IntersectionObserver)
    // Elements with [data-reveal] or [data-reveal-stagger] fade in when they
    // enter the viewport. One-shot: once revealed, the observer disconnects
    // for that element.
    // -----------------------------------------------------------------------

    function initScrollReveal() {
        var targets = document.querySelectorAll('[data-reveal], [data-reveal-stagger]');
        if (!targets.length) {
            return;
        }

        var observer = new IntersectionObserver(function(entries) {
            entries.forEach(function(entry) {
                if (entry.isIntersecting) {
                    entry.target.classList.add('revealed');
                    observer.unobserve(entry.target);
                }
            });
        }, { threshold: 0.15 });

        targets.forEach(function(el) {
            observer.observe(el);
        });
    }

    // -----------------------------------------------------------------------
    // Telemetry Simulation
    // Simulated build log that auto-types lines like a mainframe printout.
    // After all lines are displayed, pauses, clears, and loops forever.
    // -----------------------------------------------------------------------

    var TELEMETRY_LINES = [
        '[14:32:01.003] engine    \u25B8 pipeline validated \u2014 7 nodes, 9 edges',
        '[14:32:01.017] engine    \u25B8 phase: EXECUTE',
        '[14:32:01.018] node:start\u25B8 entering start',
        '[14:32:01.019] node:lint \u25B8 spawning agent (model=claude-sonnet-4-5, fidelity=standard)',
        '[14:32:03.441] node:lint \u25B8 tool_call: bash ["golangci-lint run ./..."]',
        '[14:32:05.112] node:lint \u25B8 completed (2.09s, 1 tool call, 3847 tokens)',
        '[14:32:05.113] node:test \u25B8 spawning agent (model=claude-sonnet-4-5, fidelity=thorough)',
        '[14:32:07.882] node:test \u25B8 tool_call: bash ["go test -race ./..."]',
        '[14:32:11.204] node:test \u25B8 completed (6.09s, 3 tool calls, 12041 tokens)',
        '[14:32:11.205] node:build\u25B8 spawning agent (model=claude-sonnet-4-5, fidelity=standard)',
        '[14:32:13.891] node:build\u25B8 tool_call: bash ["go build -o bin/app ./cmd/app"]',
        '[14:32:14.302] node:build\u25B8 completed (3.10s, 1 tool call, 2104 tokens)',
        '[14:32:14.303] node:done \u25B8 pipeline completed \u2014 4 stages, 14.28s total',
    ];

    function initTelemetryPreview(container) {
        var lineIndex = 0;

        // Adds a single line element to the container with a fade-in class.
        function appendLine(text) {
            var line = document.createElement('div');
            line.className = 'cl-telemetry-line';
            line.textContent = text;
            container.appendChild(line);

            // Trigger reflow so the fade-in transition activates.
            void line.offsetWidth;
            line.classList.add('cl-telemetry-line-visible');
        }

        // Returns a random delay between 200ms and 600ms.
        function randomDelay() {
            return 200 + Math.floor(Math.random() * 400);
        }

        // Types lines one at a time, then pauses, clears, and loops.
        function typeNextLine() {
            if (lineIndex < TELEMETRY_LINES.length) {
                appendLine(TELEMETRY_LINES[lineIndex]);
                lineIndex++;
                setTimeout(typeNextLine, randomDelay());
            } else {
                // Pause 3 seconds, then clear and restart.
                setTimeout(function() {
                    container.innerHTML = '';
                    lineIndex = 0;
                    typeNextLine();
                }, 3000);
            }
        }

        typeNextLine();
    }

    // -----------------------------------------------------------------------
    // Initialization
    // Wire up all landing page features on DOMContentLoaded.
    // -----------------------------------------------------------------------

    document.addEventListener('DOMContentLoaded', function() {
        // Scroll reveal for [data-reveal] and [data-reveal-stagger] elements.
        initScrollReveal();

        // Hero telemetry strip teletype animation.
        var strip = document.querySelector('.cl-telemetry-strip-text');
        if (strip) {
            var fullText = strip.textContent;
            strip.textContent = '';
            teletype(strip, fullText, 30);
        }

        // Telemetry preview simulation.
        var telemetryOutput = document.querySelector('.cl-telemetry-output');
        if (telemetryOutput) {
            initTelemetryPreview(telemetryOutput);
        }
    });
})();
