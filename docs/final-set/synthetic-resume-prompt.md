# Prompt: synthetic resumes for the final evaluation set

Give the prompt below to a separate agent. It must not see this
repository, its fixtures, labels, evaluation results, or the target job
postings: the resumes are held-out test data, and anything that hints at
how the scorer works would bias them. The prompt tells it to write only
the four output files below and to read nothing else in the repository.
Review the files before committing them.

---

You are writing three realistic, fictional software-industry resumes that
will be used as test data for a resume-to-job matching system. Write them
the way real candidates write their own resumes: not optimized for any
particular job, not keyword-stuffed, with the uneven detail real resumes
have (some bullets specific and measurable, some vague).

## The three candidates

1. **Mid-level frontend-leaning full-stack engineer**, about 4–5 years of
   experience. Strongest in TypeScript and React; some Node.js backend
   work and light cloud exposure. Has worked at a small agency and a
   mid-size SaaS company. Currently employed.
2. **Career changer into backend engineering**, about 7 years total: the
   first ~4 years in QA / test automation or technical support, the last
   ~2–3 years as a junior-to-mid backend developer (pick one main backend
   language). Their QA background shows in testing and reliability work.
   Has a gap of 6–10 months that ended recently or is ongoing (pick one;
   if ongoing, the latest role ended in 2026).
3. **Senior data / ML engineer**, about 9–11 years. Python, SQL, data
   pipelines, some ML model deployment; worked at a fintech and a retail
   or logistics company. Real strengths but visible gaps: little
   general-purpose backend service work, no front-end work, thin cloud
   infrastructure ownership. Has one short tenure (under 9 months).

Make the three clearly different from each other in stack, domain, and
seniority. Avoid making any of them a generic "Java/Spring/Kafka backend
engineer".

## Content rules

- Everything is fictional: invent names, companies, schools, and
  products; do not use real employers or real people. Use plausible US
  city and state locations (different for each candidate).
- Dates run up to October 2026, formatted like `Mar 2022 – Apr 2025` or
  `Jun 2025 – Present`. Keep them consistent (no overlaps unless clearly
  part-time).
- Include, in this order: a Summary (2–4 sentences), Skills (grouped
  lines such as `Languages: ...`), Experience (3–5 roles with 3–7 bullets
  each, newest first), Education, and optionally Certifications or
  Projects.
- Mix strong, specific bullets (what was built, with what, and a result)
  with weaker ones (responsibilities, "worked on", "familiar with"). List
  a few skills in Skills that the experience does not demonstrate, as real
  candidates do.
- Length: roughly one page each (400–700 words).
- No contact details beyond the name and location (no email, phone, or
  links).

## Output files

Write exactly these files and nothing else. Do not read, list, or open
any other file or directory in this location; create the files directly.

- `testdata/resumes/final-syn-1.md`:
  candidate 1 (frontend-leaning full-stack engineer)
- `testdata/resumes/final-syn-2.md`:
  candidate 2 (career changer into backend)
- `testdata/resumes/final-syn-3.md`:
  candidate 3 (senior data / ML engineer)
- `docs/final-set/synthetic-resume-notes.md`:
  one short paragraph per candidate with the strengths and gaps you
  intended (kept separately; not part of any resume)

If a file already exists, stop and report it instead of overwriting it.

## Format (exactly)

Each resume file contains only the resume, as plain Markdown (no code
fences, no commentary), in this structure with `|` separators in role
headers:

```
Firstname Lastname
Headline (e.g. Senior Data Engineer)
City, ST

# Summary
Two to four sentences.

# Skills
- Languages: ...
- Frameworks: ...
- Tools: ...

# Experience
## Job Title | Company Name | Mon YYYY – Mon YYYY
- Bullet
- Bullet

## Job Title | Company Name | Mon YYYY – Present
- Bullet

# Education
## Degree, Field | School Name | YYYY – YYYY
```

Finish by replying with the four file paths you wrote.
