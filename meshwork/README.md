# Meshwork in House Price Predictor

## Run the first transition

From the repository root, use Go 1.23 or later and the project's Python 3.11 environment. The transition needs the NumPy and pandas versions in `requirements.txt`.

```sh
uv venv --python 3.11
uv pip install --python .venv/bin/python numpy==1.24.3 pandas==1.5.3
go run ./meshwork -python .venv/bin/python -scenario reproduce
```

The `reproduce` scenario checks both the checked-in raw CSV digest and the cleaned CSV digest from a direct Python run. It was verified on Linux with LF line endings. It writes `data/processed/cleaned_house_data.csv` and prints the artifact SHA-256 digests and typed Meshwork token hashes as JSON. Process logs go to stderr. Use the default `clean` scenario for another raw CSV.

```sh
go run ./meshwork -python .venv/bin/python -input path/to/raw.csv -output path/to/cleaned.csv
go test ./...
```

The Go tests exercise the linked graph with a shell subprocess. One test writes a partial file and exits nonzero, checks that the raw token survives and the output place stays empty, then fixes the cause and retries the same firing. The retry uses the original snapshot even after the source file is edited. Another test checks that a zero exit with a missing output fails.

## Repository inspection

| Entry point | Inputs | Outputs and integration boundary |
| --- | --- | --- |
| `src/data/run_processing.py` | Checked-in `data/raw/house_data.csv`, pandas, NumPy | Cleans missing values and price outliers. The executable uses fixed relative paths and writes `data/processed/cleaned_house_data.csv`. This is the first transition. |
| `src/features/engineer.py` | Cleaned CSV, pandas, NumPy, scikit-learn, joblib | Writes the featured CSV and a fitted `preprocessor.pkl` via CLI arguments. Treat these as two separate output tokens. |
| `notebooks/00_*.ipynb` to `03_*.ipynb` | Raw, cleaned, or featured CSV, depending on the notebook | The notebooks support data engineering, exploration, feature design, and model comparison. `03_experimentation.ipynb` generates `configs/model_config.yaml`. The config is absent from the checkout. |
| `src/models/train_model.py` | Featured CSV, model config, scikit-learn/XGBoost, joblib, MLflow | Fits a model, logs metrics, registers a version, promotes it to Staging, and writes `models/trained/<name>.pkl`. The registry operations are external effects. |
| `src/api/main.py` | `models/trained/house_price_model.pkl`, `preprocessor.pkl`, requests validated by Pydantic | FastAPI exposes `/health`, `/predict`, and `/batch-predict`. The model and preprocessor are loaded at import time. API dependencies are in `src/api/requirements.txt`. |
| `streamlit_app/app.py` | Form values, `API_URL`, Streamlit and Requests | Calls FastAPI's `/predict` and displays the result. It displays mock values when the request fails. |
| `deployment/mlflow/docker-compose.yaml` | Docker Compose and the MLflow image | Starts MLflow on port 5555 with a SQLite backend and a container-local artifact directory. The checkout has a Kubernetes README stub and instructions to create the app Dockerfiles. GitHub Actions is described in the project README. |

The Python scripts and notebooks use the dependencies in the root `requirements.txt`. Streamlit has its own `requirements.txt`. The base checkout `cf781ab` has no checked-in app Dockerfiles, GitHub Actions workflows, or automated tests.

## What the first transition uses

[Meshwork `0ba9c7f`](https://github.com/cakesmith/meshwork/commit/0ba9c7f0530ccb49d3c9b900492e9022e97f884f) already provides the Go linker, typed graph composition, 1-safe atomic firing, token hashing, and a finite scenario runner. It also has streaming scenarios and an HTTP executor protocol. Its scenarios lack expected process-failure assertions and retry policy. The house-price integration pins that revision and supplies the local subprocess binding through the existing Go implementation registry.

`graph.house-price-training.meshwork.yaml` binds its two typed places to the reusable `graph.process-house-data.meshwork.yaml` subgraph. The subgraph contains one executable transition. `local.scenarios.house-price-training.meshwork.yaml` holds the run and reproducibility cases.

The adapter takes an executable and argument vector, maps input and output ports to files, and stages each firing in a fresh directory. The script runs as-is with its expected relative paths. Each artifact is captured under its content digest in a temporary store for the run. A place's token contains the digest and has its own hash derived from its type and value. A nonzero exit or missing output leaves the marking intact. Meshwork commits the output token after validation, then the runner publishes a copy of the CSV at the requested path. Staging isolates the script's normal file writes. Subprocesses have the caller's OS permissions, and effects outside staging are outside the marking transaction.

## Integration sequence

1. Add a GitHub Actions job that installs Go and the pinned Python dependencies, then runs the same `reproduce` command and Go tests. Use an isolated output path for CI.
2. Bind `engineer.py` to the cleaned CSV place and add typed outputs for the featured CSV and preprocessor. Make the reference year an explicit input before pinning feature hashes, since the script currently reads the wall clock.
3. Supply an explicit model config artifact and MLflow endpoint to a training transition. Exercise registration against the existing Compose service before adding retries. Each retry can create a new MLflow run or model version. The featured data derives `price_per_sqft` from `price`, while inference substitutes zero for that feature. Carry that training and serving difference into integration checks.
4. Expose selected graph boundaries through CLI commands and opt-in FastAPI routes. Use the declared input and output places as the contract and attach value schemas where HTTP validation is needed. Each request gets an independent marking. Keep model prediction on the existing FastAPI functions when binding it to a slice.
