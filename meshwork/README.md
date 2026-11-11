# Meshwork in House Price Predictor

## Run the first transition

From the repository root, set up the Python 3.11 environment and install the standalone Meshwork CLI with Go 1.23 or later. Installing into the virtual environment makes both `meshwork` and the selected `python` available on `PATH`.

```sh
uv venv --python 3.11
. .venv/bin/activate
uv pip install numpy==1.24.3 pandas==1.5.3
GOBIN="$PWD/.venv/bin" go install github.com/cakesmith/meshwork/cmd/meshwork@bf3a03ca92f8c62311624193420a02a215180010
meshwork execute --scenario meshwork/verify.scenario.house-price-training.meshwork.yaml \
  --subprocess meshwork/subprocess.yaml meshwork
```

The `reproduce` scenario checks both the checked-in raw CSV digest and the cleaned CSV digest from a direct Python run. It was verified on Linux with LF line endings. It writes `data/processed/cleaned_house_data.csv` and prints the artifact SHA-256 digests and typed Meshwork token hashes as JSON. Process logs go to stderr.

```sh
meshwork execute --scenario meshwork/run.scenario.house-price-training.meshwork.yaml \
  --subprocess meshwork/subprocess.yaml meshwork
```

For a different CSV, set the host paths under the top-level `inputs` and `outputs` in `meshwork/subprocess.yaml`, then use the `clean` scenario. The implementation's paths name files inside the private working directory where Python runs.

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

The standalone Meshwork package provides the linker, scenario runner, subprocess adapter, and artifact handling. This repository supplies the graph and scenario YAML and binds `house-data.process` to `src/data/run_processing.py` in `meshwork/subprocess.yaml`.

`graph.house-price-training.meshwork.yaml` binds its two typed places to the reusable `graph.process-house-data.meshwork.yaml` subgraph. The subgraph contains one executable transition. The `run` and `verify` scenario files select that same graph.

The adapter takes an executable and argument vector, maps input and output ports to files, and stages each firing in a fresh directory. The script runs as-is with its expected relative paths. Each artifact is captured under its content digest in a temporary store for the run. A place's token contains the digest and has its own hash derived from its type and value. A nonzero exit or missing output leaves the marking intact. Meshwork commits the output token after validation, then the runner publishes a copy of the CSV at the requested path. Staging isolates the script's normal file writes. Subprocesses have the caller's OS permissions, and effects outside staging are outside the marking transaction.

## Integration sequence

1. Add a GitHub Actions job that installs the pinned Meshwork CLI and Python dependencies, then runs the same `reproduce` scenario. Use an isolated output path for CI.
2. Bind `engineer.py` to the cleaned CSV place and add typed outputs for the featured CSV and preprocessor. Make the reference year an explicit input before pinning feature hashes, since the script currently reads the wall clock.
3. Supply an explicit model config artifact and MLflow endpoint to a training transition. Exercise registration against the existing Compose service before adding retries. Each retry can create a new MLflow run or model version. The featured data derives `price_per_sqft` from `price`, while inference substitutes zero for that feature. Carry that training and serving difference into integration checks.
4. Expose selected graph boundaries through CLI commands and opt-in FastAPI routes. Use the declared input and output places as the contract and attach value schemas where HTTP validation is needed. Each request gets an independent marking. Keep model prediction on the existing FastAPI functions when binding it to a slice.
