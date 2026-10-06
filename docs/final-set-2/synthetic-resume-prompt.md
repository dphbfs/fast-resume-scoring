# Prompt: synthetic resumes for the second sealed final set

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

1. **New-grad backend engineer**, about 1–2 years of experience after a
   CS degree (plus one internship). Main language Go or Java (pick one);
   some cloud and SQL from coursework and the first job. Currently
   employed at a mid-size company.
2. **Staff-level platform / SRE engineer**, about 12–14 years. Kubernetes,
   Terraform, observability, incident management, internal developer
   platforms; started as a Linux sysadmin. Little product feature work;
   writes mostly Go and Python tooling. Has led teams without a manager
   title.
3. **Application / cloud security engineer**, about 6–8 years. Started
   as a Python developer, then moved into security: threat modeling,
   secure code review, AWS IAM and cloud security posture, SAST/DAST
   tooling, incident response. Some detection engineering; no
   front-end work. Has one contract stint between two full-time roles.

Make the three clearly different from each other in stack, domain, and
seniority. Avoid making any of them a generic "Java/Spring/Kafka backend
engineer" or a full-stack TypeScript/React engineer.

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

- `testdata/resumes/final2-syn-1.md`:
  candidate 1 (new-grad backend engineer)
- `testdata/resumes/final2-syn-2.md`:
  candidate 2 (staff platform / SRE engineer)
- `testdata/resumes/final2-syn-3.md`:
  candidate 3 (application / cloud security engineer)
- `docs/final-set-2/synthetic-resume-notes.md`:
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
