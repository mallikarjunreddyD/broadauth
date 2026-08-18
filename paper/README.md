# Prob-Adaptive Inf-TESLA++ — manuscript

`prob-adaptive-inf-tesla.tex` is a self-contained IEEEtran conference paper
rewritten around the EIP-1559 proportional controller that is actually
implemented, and the live end-to-end radio-budget measurements.

## Compile

No LaTeX toolchain is installed locally. Easiest path:

1. **Overleaf** — create a project, upload `prob-adaptive-inf-tesla.tex` and the
   `figures/` folder, set the compiler to pdfLaTeX. It uses only stock packages
   (`IEEEtran`, `amsmath`, `algorithm`, `algorithmic`, `graphicx`, `booktabs`,
   `amsthm`). The four figures it needs are `fig_slot_budget.png`,
   `fig_backlog.png`, `fig_verification.png`, `fig_timeseries.png`.
2. **Local** (after `brew install --cask mactex-no-gui` or `tlmgr`):
   ```bash
   cd paper && pdflatex prob-adaptive-inf-tesla.tex && pdflatex prob-adaptive-inf-tesla.tex
   ```

## Before submitting

- Fill in the author block (`[Author Names]`, institution, emails).
- The figures in `figures/` are the N=3 median±range end-to-end results
  (regenerate with `benchmarking/generate_e2e_charts.py` if the data changes).
- Known caveats are stated in Section VIII (Discussion) — slot-counter timing
  (F8.5), single-node loopback, finite-run steady state, Bloom-filter growth.
  Fixing F8.5 and adding a fixed-slot baseline arm are the highest-value next
  steps before camera-ready.
- The reference list uses only well-established real works plus the Inf-TESLA++
  baseline; verify the Inf-TESLA++ citation details against your copy.
